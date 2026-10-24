package brain

import (
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
	b.events.Publish(events.Event{Type: typ, Path: bf.Path, Chunks: chunkIDs(bf.Path, bf.Content), Source: source})
}

// emitDelete announces a removed note, with the chunk ids it had in the index.
// Caller holds mu.
func (b *FileBrain) emitDelete(path string) {
	if b.events == nil || IsVaultPath(path) {
		return
	}
	var chunks []string
	if idx, err := b.loadFullIndexCached(); err == nil && idx != nil {
		for i := 0; i < idx.Documents[path].ChunkCount; i++ {
			chunks = append(chunks, fmt.Sprintf("%s#%d", path, i))
		}
	}
	b.events.Publish(events.Event{Type: events.Delete, Path: path, Chunks: chunks, Source: b.eventSource})
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
