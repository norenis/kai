---
tags: [go, programming, concurrency, testing]
confidence: low
source: mcp
updated: 2026-10-24
---

# Context cancellation patterns

The go.mod file pins the minimum language version, and the toolchain line pins the compiler. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause.

## Open questions

Close the response body even on error paths, or the transport cannot reuse the connection. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates.

Keep the cmd package thin and put the real logic in internal packages that are easy to test. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops. Close the response body even on error paths, or the transport cannot reuse the connection.
