package brain

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/norenis/kai/internal/events"
)

// SetEvents makes the brain publish its writes, deletes and reads on bus.
// source labels events whose writer did not say who it is (BrainFile.WrittenBy).
func (b *FileBrain) SetEvents(bus *events.Bus, source string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = bus
	b.eventSource = source
}

// Events returns the bus set by SetEvents, or nil.
func (b *FileBrain) Events() *events.Bus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.events
}

// chunkIDs returns the chunk keys a note's content splits into, matching the
// keys the index will give it ("path#0", "path#1", ...).
func chunkIDs(path, content string) []string {
	n := len(chunkContent(strings.TrimSpace(content), ChunkTargetWords))
	if n == 0 {
		n = 1
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s#%d", path, i)
	}
	return ids
}

// emit publishes an event about one note. Vault notes are never announced:
// the stream is visible to anyone who can open the dashboard. Caller holds mu.
func (b *FileBrain) emit(typ string, bf *BrainFile, source string) {
	if b.events == nil || bf == nil || IsVaultPath(bf.Path) {
		return
	}
	if source == "" {
		source = bf.WrittenBy
	}
	if source == "" {
		source = b.eventSource
	}
	b.events.Publish(events.Event{Type: typ, Path: bf.Path, Title: Title(bf.Path, bf.Content), Chunks: chunkIDs(bf.Path, bf.Content), Source: source})
}

// emitDelete announces a removed note, with the chunk ids it had in the index.
// Caller holds mu.
func (b *FileBrain) emitDelete(path string) {
	if b.events == nil || IsVaultPath(path) {
		return
	}
	title, chunks := b.indexedNote(path)
	b.events.Publish(events.Event{Type: events.Delete, Path: path, Title: title, Chunks: chunks, Source: b.eventSource})
}

// exists reports whether a note is already on disk. Caller holds mu.
func (b *FileBrain) exists(relPath string) bool {
	p, err := b.safePath(relPath)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// WatchDisk reports notes changed by other processes (CLI, git sync, an
// editor) on the brain's event bus until ctx ends. No-op without a bus.
func (b *FileBrain) WatchDisk(ctx context.Context) error {
	bus := b.Events()
	if bus == nil {
		return nil
	}
	_, err := events.Watch(ctx, b.root, bus, events.WatchOptions{
		Describe: b.describe,
		Skip:     IsVaultPath,
	})
	return err
}

// describe returns a note's title and chunk ids from disk, or from the
// index when the note was deleted.
func (b *FileBrain) describe(path string, deleted bool) (string, []string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if deleted {
		return b.indexedNote(path)
	}
	bf, err := b.load(path)
	if err != nil {
		return FilenameTitle(path), nil
	}
	return Title(path, bf.Content), chunkIDs(path, bf.Content)
}

// indexedNote returns the title and chunk ids the index holds for a note.
// Caller holds mu.
func (b *FileBrain) indexedNote(path string) (string, []string) {
	idx, err := b.loadFullIndexCached()
	if err != nil || idx == nil {
		return FilenameTitle(path), nil
	}
	d := idx.Documents[path]
	ids := make([]string, d.ChunkCount)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s#%d", path, i)
	}
	if d.Path == "" {
		return FilenameTitle(path), ids
	}
	return d.DisplayTitle(), ids
}
