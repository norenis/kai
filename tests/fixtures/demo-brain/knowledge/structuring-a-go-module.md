---
tags: [go, programming, tooling]
confidence: high
source: import
updated: 2026-08-17
access_count: 8
---

# Structuring a Go module

Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other. Wrap errors with %w so callers can still use errors.Is and errors.As on the original cause. The go.mod file pins the minimum language version, and the toolchain line pins the compiler. Pass a context.Context as the first argument so cancellation and deadlines flow through the whole call chain.

## Checklist

- Use a sync.Mutex when goroutines share state, and a channel when they hand ownership of data to each other.
- Keep the cmd package thin and put the real logic in internal packages that are easy to test.
- Always set ReadHeaderTimeout on an http.Server, otherwise slow clients can hold connections open forever.
- Interfaces belong to the consumer package and should stay small, one or two methods at most.
