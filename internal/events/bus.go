// Package events carries brain activity (writes, reads, searches) from the
// places it happens to live views such as the dashboard's event stream.
package events

import (
	"context"
	"sync"
	"time"
)

// Event types.
const (
	Learn  = "learn"
	Create = "create"
	Update = "update"
	Delete = "delete"
	Search = "search"
	Ask    = "ask"
	Touch  = "touch"
)

// Event sources.
const (
	SourceMCP       = "mcp"
	SourceDashboard = "dashboard"
	SourceCLI       = "cli"
	SourceFS        = "fs" // a write made by another process, seen on disk
)

// Event is one thing that happened to the brain.
type Event struct {
	ID     uint64    `json:"id"` // increasing per bus; used for SSE replay
	Type   string    `json:"type"`
	Path   string    `json:"path,omitempty"`
	Chunks []string  `json:"chunks,omitempty"` // chunk ids "path#n"
	Query  string    `json:"query,omitempty"`
	Trace  *Trace    `json:"trace,omitempty"` // search/ask only
	Source string    `json:"source"`
	TS     time.Time `json:"ts"`
}

// Trace times the stages of a search or ask.
type Trace struct {
	Stages  []Stage `json:"stages"`
	TotalMS float64 `json:"total_ms"`
}

// Stage is one timed step of a trace.
type Stage struct {
	Name  string  `json:"name"`
	MS    float64 `json:"ms"`
	Count int     `json:"count"`
}

// RecentSize is how many events the bus keeps for replay.
const RecentSize = 200

// subscriberBuffer is how far a subscriber may fall behind before it is dropped.
const subscriberBuffer = 64

type subscriber struct {
	ch chan Event
}

// Bus fans events out to subscribers without ever blocking a publisher: a
// subscriber whose buffer is full is dropped (its channel is closed) and is
// expected to reconnect and replay from Recent. All methods are safe on a
// nil *Bus, which simply discards events.
type Bus struct {
	mu        sync.Mutex
	nextID    uint64
	ring      [RecentSize]Event
	size      int // events in ring
	head      int // index of the oldest event
	subs      map[*subscriber]struct{}
	lastByKey map[string]time.Time // path → last publish time (for fs dedupe)
	now       func() time.Time
}

// NewBus returns an empty bus.
func NewBus() *Bus {
	return &Bus{subs: map[*subscriber]struct{}{}, lastByKey: map[string]time.Time{}, now: time.Now}
}

// Publish records e and delivers it to every subscriber. ID and TS are set
// by the bus (TS only when zero). Never blocks.
func (b *Bus) Publish(e Event) Event {
	if b == nil {
		return e
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	e.ID = b.nextID
	if e.TS.IsZero() {
		e.TS = b.now().UTC()
	}
	if b.size < RecentSize {
		b.ring[(b.head+b.size)%RecentSize] = e
		b.size++
	} else {
		b.ring[b.head] = e
		b.head = (b.head + 1) % RecentSize
	}
	if e.Path != "" {
		b.lastByKey[e.Path] = b.now()
		if len(b.lastByKey) > 4*RecentSize {
			b.pruneKeys()
		}
	}

	for s := range b.subs {
		select {
		case s.ch <- e:
		default:
			// Too slow: drop it rather than hold up the writer.
			delete(b.subs, s)
			close(s.ch)
		}
	}
	return e
}

// pruneKeys forgets paths published more than a minute ago. Caller holds mu.
func (b *Bus) pruneKeys() {
	cutoff := b.now().Add(-time.Minute)
	for k, t := range b.lastByKey {
		if t.Before(cutoff) {
			delete(b.lastByKey, k)
		}
	}
}

// Subscribe returns a channel of events published from now on. The channel
// closes when ctx ends or when the subscriber falls too far behind.
func (b *Bus) Subscribe(ctx context.Context) <-chan Event {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	s := &subscriber{ch: make(chan Event, subscriberBuffer)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(s.ch)
		}
	}()
	return s.ch
}

// Recent returns the retained events, oldest first.
func (b *Bus) Recent() []Event {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, b.size)
	for i := 0; i < b.size; i++ {
		out[i] = b.ring[(b.head+i)%RecentSize]
	}
	return out
}

// PublishedWithin reports whether an event for path was published within d.
// The filesystem watcher uses it to skip writes this process already reported.
func (b *Bus) PublishedWithin(path string, d time.Duration) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.lastByKey[path]
	return ok && b.now().Sub(t) <= d
}

// Subscribers returns the number of live subscribers.
func (b *Bus) Subscribers() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
