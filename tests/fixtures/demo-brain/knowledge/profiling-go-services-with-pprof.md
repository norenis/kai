---
tags: [go, programming, performance, tooling, errors]
confidence: medium
source: import
updated: 2026-08-22
access_count: 8
---

# Profiling Go services with pprof

Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. Close the response body even on error paths, or the transport cannot reuse the connection. Keep the cmd package thin and put the real logic in internal packages that are easy to test.

## Pitfalls

Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. Keep the cmd package thin and put the real logic in internal packages that are easy to test. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive.

Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Close the response body even on error paths, or the transport cannot reuse the connection.

## Notes

A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops. Interfaces belong to the consumer package and should stay small, one or two methods at most. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Close the response body even on error paths, or the transport cannot reuse the connection. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause.

Keep the cmd package thin and put the real logic in internal packages that are easy to test. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.

## Checklist

- A goroutine is cheap to start, but every goroutine needs a clear owner that decides when it stops.
- The go.mod file pins the minimum language version, and the toolchain line pins the compiler.
- The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive.
- Keep the cmd package thin and put the real logic in internal packages that are easy to test.
- Benchmarks need b.ReportAllocs to show whether an optimisation actually reduced allocations.
