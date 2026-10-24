---
tags: [go, programming, concurrency]
confidence: medium
source: import
updated: 2026-10-10
access_count: 13
---

# Error wrapping with fmt.Errorf

Run go vet and the race detector in CI; data races rarely show up in local manual testing. Keep the cmd package thin and put the real logic in internal packages that are easy to test. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change.

## Steps

- Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read.
- A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.
- The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive.

## Why it matters

A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. Close the response body even on error paths, or the transport cannot reuse the connection. Benchmarks need b.ReportAllocs to show whether an optimisation actually reduced allocations.

Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. The go.mod file pins the minimum language version, and the toolchain line pins the compiler. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops. Keep the cmd package thin and put the real logic in internal packages that are easy to test.

## Notes

Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. Interfaces belong to the consumer package and should stay small, one or two methods at most. The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive.

Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Run go vet and the race detector in CI; data races rarely show up in local manual testing. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates.
