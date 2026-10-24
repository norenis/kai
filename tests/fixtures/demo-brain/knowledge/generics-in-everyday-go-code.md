---
tags: [go, programming, concurrency, tooling, testing]
confidence: medium
source: conversation
updated: 2026-07-10
access_count: 2
---

# Generics in everyday Go code

Interfaces belong to the consumer package and should stay small, one or two methods at most. Close the response body even on error paths, or the transport cannot reuse the connection. Run go vet and the race detector in CI; data races rarely show up in local manual testing.

## Checklist

- Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain.
- A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.
- Table driven tests keep each case as a struct literal, which makes new edge cases a one line change.

## Pitfalls

Close the response body even on error paths, or the transport cannot reuse the connection. Run go vet and the race detector in CI; data races rarely show up in local manual testing. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain.

Run go vet and the race detector in CI; data races rarely show up in local manual testing. Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other.
