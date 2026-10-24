package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/norenis/kai/internal/events"
)

// sseHeartbeat keeps idle connections alive through proxies.
var sseHeartbeat = 25 * time.Second

// SetEvents connects the dashboard to the brain's event bus.
func (s *Server) SetEvents(bus *events.Bus) {
	s.events = bus
}

// handleEvents streams brain events as Server-Sent Events: first the
// retained recent events (only those after Last-Event-ID when the browser
// reconnects), then live ones, with a comment heartbeat every 25 s.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx-style proxies: do not buffer

	// Subscribe before reading Recent so nothing falls in between; the id
	// check below drops the overlap.
	live := s.events.Subscribe(r.Context())

	var lastID uint64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		lastID, _ = strconv.ParseUint(v, 10, 64)
	}
	fmt.Fprint(w, "retry: 3000\n\n")
	for _, e := range s.events.Recent() {
		if e.ID > lastID {
			writeEvent(w, e)
			lastID = e.ID
		}
	}
	flusher.Flush()

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-live:
			if !ok {
				return // dropped as too slow, or no bus; the browser reconnects
			}
			if e.ID <= lastID {
				continue
			}
			writeEvent(w, e)
			lastID = e.ID
			flusher.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, e events.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, data)
}
