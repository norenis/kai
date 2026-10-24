package events

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchOptions tunes the filesystem watcher. Zero values use the defaults.
type WatchOptions struct {
	Debounce time.Duration // quiet time per path before reporting (250 ms)
	Dedupe   time.Duration // skip paths this process published within (2 s)
	MaxRate  int           // events per second at most (20)
	// Describe returns a note's title and chunk ids; deleted notes are looked
	// up in the index since the file is gone. Optional.
	Describe func(path string, deleted bool) (title string, chunks []string)
	// Skip reports paths that must never be announced (e.g. the vault). Optional.
	Skip func(path string) bool
}

// ignoredDirs never hold notes: the trash, version control, tooling.
var ignoredDirs = map[string]bool{".trash": true, ".git": true, "node_modules": true}

// isNotePath reports whether a brain-relative path is a markdown note the
// watcher should report.
func isNotePath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".md") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if ignoredDirs[part] || strings.HasPrefix(part, ".") {
			return false
		}
	}
	base := filepath.Base(rel)
	return !strings.HasPrefix(base, "~") && !strings.HasSuffix(base, "~")
}

// Watcher reports note changes made by other processes (the CLI, git sync,
// an editor) as fs events on the bus.
type Watcher struct {
	root string
	bus  *Bus
	opt  WatchOptions
	fw   *fsnotify.Watcher

	mu      sync.Mutex
	known   map[string]bool        // notes on disk, brain-relative
	pending map[string]*time.Timer // per-path debounce

	recent  []time.Time // publish times within the last second (rate cap)
	dropped int
}

// Watch starts watching root recursively until ctx ends.
func Watch(ctx context.Context, root string, bus *Bus, opt WatchOptions) (*Watcher, error) {
	if opt.Debounce == 0 {
		opt.Debounce = 250 * time.Millisecond
	}
	if opt.Dedupe == 0 {
		opt.Dedupe = 2 * time.Second
	}
	if opt.MaxRate == 0 {
		opt.MaxRate = 20
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{root: root, bus: bus, opt: opt, fw: fw, known: map[string]bool{}, pending: map[string]*time.Timer{}}
	if err := w.addTree(root, false); err != nil {
		fw.Close()
		return nil, err
	}
	go w.loop(ctx)
	return w, nil
}

// addTree watches dir and its subdirectories; with announce set, notes found
// inside (a directory moved or copied in) are reported as new.
func (w *Watcher) addTree(dir string, announce bool) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // vanished meanwhile
		}
		rel, _ := filepath.Rel(w.root, p)
		if d.IsDir() {
			if p != w.root && (ignoredDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if err := w.fw.Add(p); err != nil {
				slog.Warn("fswatch: cannot watch directory", "dir", p, "err", err)
			}
			return nil
		}
		if isNotePath(rel) {
			if announce {
				w.schedule(filepath.ToSlash(rel))
			} else {
				w.mu.Lock()
				w.known[filepath.ToSlash(rel)] = true
				w.mu.Unlock()
			}
		}
		return nil
	})
}

func (w *Watcher) loop(ctx context.Context) {
	defer w.fw.Close()
	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			for _, t := range w.pending {
				t.Stop()
			}
			w.mu.Unlock()
			return
		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			slog.Warn("fswatch error", "err", err)
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	if ev.Has(fsnotify.Create) {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			if !ignoredDirs[fi.Name()] && !strings.HasPrefix(fi.Name(), ".") {
				_ = w.addTree(ev.Name, true)
			}
			return
		}
	}
	if isNotePath(rel) {
		w.schedule(filepath.ToSlash(rel))
	}
}

// schedule (re)starts the debounce timer for a path.
func (w *Watcher) schedule(rel string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.pending[rel]; ok {
		t.Reset(w.opt.Debounce)
		return
	}
	w.pending[rel] = time.AfterFunc(w.opt.Debounce, func() { w.flush(rel) })
}

// flush decides what happened to a path once it has been quiet: the file's
// presence now, compared with what was known, gives create, update or delete.
func (w *Watcher) flush(rel string) {
	_, statErr := os.Stat(filepath.Join(w.root, filepath.FromSlash(rel)))
	exists := statErr == nil

	w.mu.Lock()
	delete(w.pending, rel)
	wasKnown := w.known[rel]
	if exists {
		w.known[rel] = true
	} else {
		delete(w.known, rel)
	}
	w.mu.Unlock()

	var typ string
	switch {
	case exists && wasKnown:
		typ = Update
	case exists:
		typ = Create
	case wasKnown:
		typ = Delete
	default:
		return // a temp file that came and went
	}
	if w.opt.Skip != nil && w.opt.Skip(rel) {
		return
	}
	// This process already reported it (its own save landing on disk).
	if w.bus.PublishedWithin(rel, w.opt.Dedupe) {
		return
	}
	if !w.allow() {
		return
	}
	var title string
	var chunks []string
	if w.opt.Describe != nil {
		title, chunks = w.opt.Describe(rel, typ == Delete)
	}
	w.bus.Publish(Event{Type: typ, Path: rel, Title: title, Chunks: chunks, Source: SourceFS})
}

// allow caps reports at MaxRate within any one-second window, so bulk
// changes (git pull, a script) cannot flood the stream.
func (w *Watcher) allow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	keep := w.recent[:0]
	for _, t := range w.recent {
		if now.Sub(t) < time.Second {
			keep = append(keep, t)
		}
	}
	w.recent = keep
	if len(w.recent) >= w.opt.MaxRate {
		w.dropped++
		if w.dropped == 1 || w.dropped%100 == 0 {
			slog.Info("fswatch: rate limited", "dropped", w.dropped)
		}
		return false
	}
	w.recent = append(w.recent, now)
	return true
}
