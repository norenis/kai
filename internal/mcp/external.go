package mcp

import (
	"context"
	"time"

	"github.com/norenis/kai/internal/events"
)

// externalReindexDelay groups writes from other processes (a git pull, a
// task sync) into one background reindex.
const externalReindexDelay = 30 * time.Second

// reindexOnExternalWrites triggers a reindex once notes changed by other
// processes have been quiet for delay, so their memories reach the sky.
// Embeddings are reused per chunk content, so only changed chunks are
// embedded again.
func reindexOnExternalWrites(ctx context.Context, bus *events.Bus, delay time.Duration, trigger func()) {
	timer := time.NewTimer(delay)
	timer.Stop()
	for {
		sub := bus.Subscribe(ctx)
	read:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				trigger()
			case e, ok := <-sub:
				if !ok {
					break read // dropped as too slow: subscribe again
				}
				if e.Source == events.SourceFS && e.Type != events.Touch {
					timer.Reset(delay)
				}
			}
		}
	}
}
