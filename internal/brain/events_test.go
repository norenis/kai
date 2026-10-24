package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/norenis/kai/internal/events"
)

func collect(t *testing.T, bus *events.Bus) func() []events.Event {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := bus.Subscribe(ctx)
	return func() []events.Event {
		var out []events.Event
		for {
			select {
			case e := <-ch:
				out = append(out, e)
			case <-time.After(50 * time.Millisecond):
				return out
			}
		}
	}
}

func TestBrainPublishesWrites(t *testing.T) {
	b := New(t.TempDir())
	bus := events.NewBus()
	b.SetEvents(bus, events.SourceMCP)
	drain := collect(t, bus)

	long := strings.Repeat("word ", 450) // three chunks of ~200 words
	note := &BrainFile{Path: "knowledge/a.md", Tags: []string{"x"}, Confidence: "high", Content: "\n" + long + "\n", WrittenBy: events.SourceDashboard}
	if err := b.Save(note); err != nil {
		t.Fatal(err)
	}
	note.Content += "\nmore\n"
	note.WrittenBy = ""
	if err := b.Save(note); err != nil {
		t.Fatal(err)
	}
	if err := b.Learn([]Learning{{Category: "opinions", Topic: "tea", Tags: []string{"tea"}, Content: "Green tea is best.", Confidence: "medium", Action: "create"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Touch("opinions/tea.md"); err != nil {
		t.Fatal(err)
	}
	// Vault notes are never announced.
	if err := b.Save(&BrainFile{Path: "vault/secret.md", Content: "\nshh\n"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Touch("vault/secret.md"); err != nil {
		t.Fatal(err)
	}

	got := drain()
	want := []struct{ typ, path, source string }{
		{events.Create, "knowledge/a.md", events.SourceDashboard},
		{events.Update, "knowledge/a.md", events.SourceMCP},
		{events.Learn, "opinions/tea.md", events.SourceMCP},
		{events.Touch, "opinions/tea.md", events.SourceMCP},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Type != w.typ || got[i].Path != w.path || got[i].Source != w.source {
			t.Errorf("event %d = %s %s %s, want %+v", i, got[i].Type, got[i].Path, got[i].Source, w)
		}
	}

	// Chunk ids match the keys the index assigns.
	if err := b.RebuildFullIndex(); err != nil {
		t.Fatal(err)
	}
	idx, _ := b.LoadFullIndex()
	if n := idx.Documents["knowledge/a.md"].ChunkCount; len(got[1].Chunks) != n || n < 2 {
		t.Errorf("update chunks = %v, index has %d", got[1].Chunks, n)
	}
	for _, id := range got[1].Chunks {
		if _, ok := idx.Chunks[id]; !ok {
			t.Errorf("chunk %s not in index", id)
		}
	}
}

func TestBrainPublishesMovesAsCreateAndDelete(t *testing.T) {
	b := New(t.TempDir())
	if err := b.Save(&BrainFile{Path: "knowledge/old.md", Confidence: "high", Content: "\nnote body\n"}); err != nil {
		t.Fatal(err)
	}
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	b.SetEvents(bus, events.SourceDashboard)
	drain := collect(t, bus)

	plan := &ReorgPlan{Actions: []ReorgAction{{Type: ActionMove, SourcePaths: []string{"knowledge/old.md"}, TargetPath: "opinions/new.md"}}}
	if err := b.ApplyReorgPlan(plan); err != nil {
		t.Fatal(err)
	}
	got := drain()
	if len(got) != 2 || got[0].Type != events.Create || got[0].Path != "opinions/new.md" ||
		got[1].Type != events.Delete || got[1].Path != "knowledge/old.md" || len(got[1].Chunks) != 1 {
		t.Errorf("events = %+v", got)
	}
}

func TestWatchDiskOneEventPerWrite(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Save(&BrainFile{Path: "knowledge/a.md", Confidence: "high", Content: "\nfirst\n"}); err != nil {
		t.Fatal(err)
	}
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	b.SetEvents(bus, events.SourceMCP)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.WatchDisk(ctx); err != nil {
		t.Fatal(err)
	}
	drain := collect(t, bus)

	// In-process save: the brain reports it; the watcher must not repeat it.
	if err := b.Save(&BrainFile{Path: "knowledge/a.md", Confidence: "high", Content: "\nsecond\n"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	got := drain()
	if len(got) != 1 || got[0].Source != events.SourceMCP {
		t.Fatalf("in-process save: %+v", got)
	}

	// Another process (here: a second FileBrain without a bus, like `kai task add`).
	other := New(dir)
	if err := other.Save(&BrainFile{Path: "tasks/2026-10-24.md", Confidence: "medium", Content: "\n- [ ] item\n"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	got = drain()
	if len(got) != 1 || got[0].Source != events.SourceFS || got[0].Type != events.Create || got[0].Path != "tasks/2026-10-24.md" || len(got[0].Chunks) != 1 {
		t.Fatalf("external save: %+v", got)
	}

	// Reindex writes only derived files: nothing is reported.
	if err := other.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if got := drain(); len(got) != 0 {
		t.Fatalf("reindex produced events: %+v", got)
	}
}
