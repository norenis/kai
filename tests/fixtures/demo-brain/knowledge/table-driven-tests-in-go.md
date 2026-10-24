---
tags: [go, programming, concurrency]
confidence: medium
source: import
updated: 2026-06-28
access_count: 1
---

# Table driven tests in Go

Interfaces belong to the consumer package and should stay small, one or two methods at most. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain. A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates.

## Open questions

Interfaces belong to the consumer package and should stay small, one or two methods at most. Run go vet and the race detector in CI; data races rarely show up in local manual testing. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read.

Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read.

## Notes

The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive. Generic helpers such as Map and Filter are fine, but a plain for loop is often easier to read. Table driven tests keep each case as a struct literal, which makes new edge cases a one line change. Benchmarks need b.ReportAllocs to show whether an optimisation actually reduced allocations. Keep the cmd package thin and put the real logic in internal packages that are easy to test.

The go.mod file pins the minimum language version, and the toolchain line pins the compiler. Keep the cmd package thin and put the real logic in internal packages that are easy to test. Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever.

## Why it matters

A buffered channel is a queue, not a performance trick; size it from the producer and consumer rates. Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Close the response body even on error paths, or the transport cannot reuse the connection. The go.mod file pins the minimum language version, and the toolchain line pins the compiler. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain.

Close the response body even on error paths, or the transport cannot reuse the connection. The pprof CPU profile shows where time goes; the heap profile shows who keeps memory alive.

## Example

```go
func TestAdd(t *testing.T) {
	cases := []struct{ a, b, want int }{{1, 2, 3}, {0, 0, 0}}
	for _, c := range cases {
		if got := Add(c.a, c.b); got != c.want {
			t.Errorf("Add(%d, %d) = %d", c.a, c.b, got)
		}
	}
}
```
