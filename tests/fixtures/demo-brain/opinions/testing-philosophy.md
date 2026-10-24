---
tags: [opinion, testing, go]
confidence: medium
source: conversation
updated: 2026-08-26
---

# Testing philosophy

Integration tests with real files beat mocks for a small codebase.

A test that never fails is either redundant or not testing anything real.

Table driven tests are the default; write a custom helper only when the table gets unreadable.
