package events

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPublishOrderAndIDs(t *testing.T) {
	b := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := b.Subscribe(ctx)

	for i := 0; i < 10; i++ {
		b.Publish(Event{Type: Update, Path: fmt.Sprintf("knowledge/%d.md", i), Source: SourceMCP})
	}
	for i := 0; i < 10; i++ {
		e := <-ch
		if e.ID != uint64(i+1) || e.Path != fmt.Sprintf("knowledge/%d.md", i) {
			t.Fatalf("event %d = %+v", i, e)
		}
		if e.TS.IsZero() {
			t.Fatal("timestamp not set")
		}
	}
}

func TestRecentRingWraps(t *testing.T) {
	b := NewBus()
	total := RecentSize + 37
	for i := 0; i < total; i++ {
		b.Publish(Event{Type: Touch, Path: fmt.Sprint(i)})
	}
	r := b.Recent()
	if len(r) != RecentSize {
		t.Fatalf("len(Recent) = %d", len(r))
	}
	if r[0].Path != fmt.Sprint(total-RecentSize) || r[len(r)-1].Path != fmt.Sprint(total-1) {
		t.Errorf("ring holds %s..%s", r[0].Path, r[len(r)-1].Path)
	}
	for i := 1; i < len(r); i++ {
		if r[i].ID != r[i-1].ID+1 {
			t.Fatalf("ids not consecutive at %d", i)
		}
	}
}

func TestSlowSubscriberIsDroppedWithoutBlocking(t *testing.T) {
	b := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := b.Subscribe(ctx) // never read
	fast := b.Subscribe(ctx)

	got := 0
	start := time.Now()
	for batch := 0; batch < 10; batch++ {
		for i := 0; i < 32; i++ {
			b.Publish(Event{Type: Update, Path: "x"})
		}
		for i := 0; i < 32; i++ { // a reader that keeps up
			<-fast
			got++
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("publisher blocked by a slow subscriber")
	}
	if got != 320 {
		t.Errorf("fast subscriber got %d events", got)
	}
	n := 0
	for range slow {
		n++
	}
	if n != subscriberBuffer {
		t.Errorf("slow subscriber got %d events before being dropped, want %d", n, subscriberBuffer)
	}
	if b.Subscribers() != 1 {
		t.Errorf("subscribers = %d, want 1", b.Subscribers())
	}
}

func TestSubscribeEndsWithContext(t *testing.T) {
	b := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	ch := b.Subscribe(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after cancel")
	}
	if b.Subscribers() != 0 {
		t.Error("subscriber not removed")
	}
	b.Publish(Event{Type: Touch}) // must not panic on the closed channel
}

func TestPublishedWithin(t *testing.T) {
	b := NewBus()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	b.Publish(Event{Type: Update, Path: "knowledge/a.md"})
	now = now.Add(1500 * time.Millisecond)
	if !b.PublishedWithin("knowledge/a.md", 2*time.Second) {
		t.Error("expected a recent publish")
	}
	if b.PublishedWithin("knowledge/b.md", 2*time.Second) {
		t.Error("unexpected publish for another path")
	}
	now = now.Add(time.Second)
	if b.PublishedWithin("knowledge/a.md", 2*time.Second) {
		t.Error("publish should have expired")
	}
}

func TestNilBusIsSafe(t *testing.T) {
	var b *Bus
	b.Publish(Event{Type: Touch})
	if b.Recent() != nil || b.PublishedWithin("x", time.Hour) || b.Subscribers() != 0 {
		t.Error("nil bus should be empty")
	}
	if _, ok := <-b.Subscribe(context.Background()); ok {
		t.Error("nil bus subscription should be closed")
	}
}
