package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norenis/kai/internal/events"
)

func TestReindexOnExternalWrites(t *testing.T) {
	bus := events.NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	go reindexOnExternalWrites(ctx, bus, 80*time.Millisecond, func() { calls.Add(1) })
	time.Sleep(20 * time.Millisecond) // let it subscribe

	// In-process writes and reads are reindexed by their own path.
	bus.Publish(events.Event{Type: events.Update, Path: "a.md", Source: events.SourceMCP})
	bus.Publish(events.Event{Type: events.Touch, Path: "a.md", Source: events.SourceFS})
	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("triggered %d times for non-external events", n)
	}

	// A burst of external writes becomes one reindex after the quiet period.
	for i := 0; i < 5; i++ {
		bus.Publish(events.Event{Type: events.Create, Path: "tasks/x.md", Source: events.SourceFS})
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(40 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("triggered before the burst went quiet (%d)", n)
	}
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("triggered %d times, want 1", n)
	}
}
