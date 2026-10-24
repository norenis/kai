package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/events"
)

type sseClient struct {
	t      *testing.T
	cancel context.CancelFunc
	lines  chan string
}

func openSSE(t *testing.T, url string, header http.Header) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if res.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("missing X-Accel-Buffering: no")
	}
	c := &sseClient{t: t, cancel: cancel, lines: make(chan string, 256)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(cancel)
	return c
}

// next returns the next data event, skipping ids, retry lines and comments.
func (c *sseClient) next(timeout time.Duration) (events.Event, bool) {
	deadline := time.After(timeout)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return events.Event{}, false
			}
			if strings.HasPrefix(l, "data: ") {
				var e events.Event
				if err := json.Unmarshal([]byte(l[6:]), &e); err != nil {
					c.t.Fatal(err)
				}
				return e, true
			}
		case <-deadline:
			return events.Event{}, false
		}
	}
}

func (c *sseClient) waitFor(prefix string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return false
			}
			if strings.HasPrefix(l, prefix) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func newEventServer(t *testing.T, cfg *config.Config) (*httptest.Server, *brain.FileBrain, *events.Bus) {
	t.Helper()
	dir := t.TempDir()
	if cfg == nil {
		cfg = config.Defaults()
	}
	cfg.SessionsPath = t.TempDir()
	b := brain.New(dir)
	bus := events.NewBus()
	b.SetEvents(bus, events.SourceDashboard)
	srv := New(b, dir, cfg)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, b, bus
}

func TestEventsReplayThenStream(t *testing.T) {
	ts, _, bus := newEventServer(t, nil)
	bus.Publish(events.Event{Type: events.Update, Path: "knowledge/a.md", Source: events.SourceMCP})
	bus.Publish(events.Event{Type: events.Create, Path: "knowledge/b.md", Source: events.SourceMCP})

	c := openSSE(t, ts.URL+"/api/events", nil)
	for _, want := range []string{"knowledge/a.md", "knowledge/b.md"} {
		if e, ok := c.next(time.Second); !ok || e.Path != want {
			t.Fatalf("replay: got %+v, want %s", e, want)
		}
	}
	bus.Publish(events.Event{Type: events.Learn, Path: "knowledge/c.md", Source: events.SourceMCP})
	if e, ok := c.next(time.Second); !ok || e.Path != "knowledge/c.md" || e.Type != events.Learn || e.ID != 3 {
		t.Fatalf("live: %+v", e)
	}
}

func TestEventsResumeAfterLastEventID(t *testing.T) {
	ts, _, bus := newEventServer(t, nil)
	for _, p := range []string{"a", "b", "c"} {
		bus.Publish(events.Event{Type: events.Update, Path: p})
	}
	c := openSSE(t, ts.URL+"/api/events", http.Header{"Last-Event-Id": {"2"}})
	if e, ok := c.next(time.Second); !ok || e.Path != "c" {
		t.Fatalf("resume: %+v", e)
	}
}

func TestEventsHeartbeat(t *testing.T) {
	old := sseHeartbeat
	sseHeartbeat = 50 * time.Millisecond
	t.Cleanup(func() { sseHeartbeat = old })
	ts, _, _ := newEventServer(t, nil)
	c := openSSE(t, ts.URL+"/api/events", nil)
	if !c.waitFor("retry: 3000", time.Second) {
		t.Fatal("no retry line")
	}
	if !c.waitFor(": ping", time.Second) {
		t.Fatal("no heartbeat")
	}
}

func TestEventsRequireAuth(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.DashboardPIN = "1234"
	ts, _, _ := newEventServer(t, cfg)
	res, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", res.StatusCode)
	}
}

// A note added through the API reaches the stream within a second.
func TestTaskAddArrivesOnStream(t *testing.T) {
	ts, _, _ := newEventServer(t, nil)
	c := openSSE(t, ts.URL+"/api/events", nil)
	c.waitFor("retry:", time.Second)

	start := time.Now()
	res, err := http.Post(ts.URL+"/api/tasks/add", "application/json", bytes.NewBufferString(`{"text":"Water the herbs"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	e, ok := c.next(time.Second)
	if !ok {
		t.Fatalf("no event within 1s (status %d)", res.StatusCode)
	}
	if e.Type != events.Create || !strings.HasPrefix(e.Path, "tasks/") || e.Source != events.SourceDashboard || len(e.Chunks) == 0 {
		t.Errorf("event = %+v", e)
	}
	if time.Since(start) > time.Second {
		t.Error("event took longer than 1s")
	}
}
