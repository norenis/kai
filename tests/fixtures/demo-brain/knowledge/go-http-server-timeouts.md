---
tags: [go, programming, tooling, concurrency, errors]
confidence: medium
source: import
updated: 2026-09-27
access_count: 3
---

# Go HTTP server timeouts

A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. The go.mod file pins the minimum language version, and the toolchain line pins the compiler. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other.

## Why it matters

Benchmarks need b.ReportAllocs to show whether an optimisation actually reduced allocations. Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops. Run go vet and the race detector in CI; data races rarely show up in local manual testing.

Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. Keep the cmd package thin and put the real logic in internal packages that are easy to test.

## Notes

Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Run go vet and the race detector in CI; data races rarely show up in local manual testing.

Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.

## Steps

- Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever.
- Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause.
- Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain.
- A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.
- Run go vet and the race detector in CI; data races rarely show up in local manual testing.
