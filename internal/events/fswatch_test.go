package events

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startWatch(t *testing.T, opt WatchOptions) (string, *Bus, func(time.Duration) []Event) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "knowledge"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".trash"), 0o755)
	os.WriteFile(filepath.Join(dir, "knowledge", "old.md"), []byte("old"), 0o644)

	bus := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if opt.Debounce == 0 {
		opt.Debounce = 50 * time.Millisecond
	}
	if _, err := Watch(ctx, dir, bus, opt); err != nil {
		t.Fatal(err)
	}
	ch := bus.Subscribe(ctx)
	collect := func(wait time.Duration) []Event {
		var out []Event
		deadline := time.After(wait)
		for {
			select {
			case e := <-ch:
				out = append(out, e)
			case <-deadline:
				return out
			}
		}
	}
	return dir, bus, collect
}

func TestWatchReportsExternalChanges(t *testing.T) {
	dir, _, collect := startWatch(t, WatchOptions{Describe: func(p string, deleted bool) (string, []string) {
		return "T", []string{fmt.Sprintf("%s#0", p)}
	}})
	note := filepath.Join(dir, "knowledge", "new.md")

	// Atomic save (temp file + rename) as the CLI does: exactly one create.
	os.WriteFile(note+".tmp", []byte("hello"), 0o644)
	os.Rename(note+".tmp", note)
	got := collect(600 * time.Millisecond)
	if len(got) != 1 || got[0].Type != Create || got[0].Path != "knowledge/new.md" || got[0].Source != SourceFS || len(got[0].Chunks) != 1 || got[0].Title != "T" {
		t.Fatalf("create: %+v", got)
	}

	// Several quick writes collapse into one update.
	for i := 0; i < 5; i++ {
		os.WriteFile(note, []byte(fmt.Sprint("v", i)), 0o644)
		time.Sleep(10 * time.Millisecond)
	}
	got = collect(600 * time.Millisecond)
	if len(got) != 1 || got[0].Type != Update {
		t.Fatalf("update: %+v", got)
	}

	os.Remove(filepath.Join(dir, "knowledge", "old.md"))
	got = collect(600 * time.Millisecond)
	if len(got) != 1 || got[0].Type != Delete || got[0].Path != "knowledge/old.md" {
		t.Fatalf("delete: %+v", got)
	}

	// A new directory with notes inside is picked up.
	sub := filepath.Join(dir, "decisions")
	os.MkdirAll(sub, 0o755)
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(filepath.Join(sub, "d.md"), []byte("d"), 0o644)
	got = collect(600 * time.Millisecond)
	if len(got) != 1 || got[0].Path != "decisions/d.md" || got[0].Type != Create {
		t.Fatalf("new dir: %+v", got)
	}
}

func TestWatchIgnoresDerivedAndHiddenFiles(t *testing.T) {
	dir, _, collect := startWatch(t, WatchOptions{Skip: func(p string) bool { return filepath.Dir(p) == "vault" }})
	os.MkdirAll(filepath.Join(dir, "vault"), 0o755)
	time.Sleep(100 * time.Millisecond)
	for _, name := range []string{"index.json", "vectors.bin.gz", "clusters.json", "index_full.json.gz", "knowledge/x.md.tmp", ".trash/s/knowledge/y.md", "vault/secret.md"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	if got := collect(500 * time.Millisecond); len(got) != 0 {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestWatchSkipsWritesThisProcessPublished(t *testing.T) {
	dir, bus, collect := startWatch(t, WatchOptions{})
	// The in-process writer publishes, then its file lands on disk.
	bus.Publish(Event{Type: Update, Path: "knowledge/old.md", Source: SourceMCP})
	os.WriteFile(filepath.Join(dir, "knowledge", "old.md"), []byte("changed"), 0o644)
	got := collect(500 * time.Millisecond)
	if len(got) != 1 || got[0].Source != SourceMCP {
		t.Fatalf("want only the in-process event, got %+v", got)
	}
}

func TestWatchCapsBursts(t *testing.T) {
	dir, _, collect := startWatch(t, WatchOptions{})
	for i := 0; i < 100; i++ {
		os.WriteFile(filepath.Join(dir, "knowledge", fmt.Sprintf("n%03d.md", i)), []byte("x"), 0o644)
	}
	got := collect(900 * time.Millisecond)
	if len(got) == 0 || len(got) > 20 {
		t.Fatalf("burst of 100 writes produced %d events, want 1..20", len(got))
	}
}
