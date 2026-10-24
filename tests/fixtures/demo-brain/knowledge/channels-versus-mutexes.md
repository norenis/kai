---
tags: [go, programming, concurrency]
confidence: medium
source: import
updated: 2026-07-15
---

# Channels versus mutexes

Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. Benchmarks need b.ReportAllocs to show whether an optimisation actually reduced allocations.

## Why it matters

Interfaces belong to the consumer package and should stay small, one or two methods at most. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. Keep the cmd package thin and put the real logic in internal packages that are easy to test. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause.

Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. Interfaces belong to the consumer package and should stay small, one or two methods at most.

## Open questions

The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. Interfaces belong to the consumer package and should stay small, one or two methods at most. Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. The go.mod file pins the minimum language version, and the toolchain line pins the compiler.

Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Close the response body even on error paths, or the transport cannot reuse the connection. Run go vet and the race detector in CI; data races rarely show up in local manual testing.

## Checklist

- Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read.
- Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other.
- Run go vet and the race detector in CI; data races rarely show up in local manual testing.
- A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates.
