# kai - spec index
<!-- spec-index: the project summary and feature index. Details live in specs/NNN-<feature>/. Agents update one line per feature; never put a full spec here. -->

## Summary
kai is a CLI-based personal AI knowledge system ("digital twin") in Go. It learns from conversations, stores knowledge as markdown brain files with BM25 and vector indexes, and exposes it through a CLI, a web dashboard and an MCP server. LLM providers: Claude, OpenAI, Gemini, Ollama. Latest tag: v1.2.0. In progress: the Fluctlight Hub dashboard redesign on `feat/fluctlight-hub` (17 tasks landed, open boxes in the local task list under `private/`).

## Features
| ID | Feature | Status | Spec |
|---|---|---|---|
| 001 | Fluctlight Hub dashboard (Celestial Orrery) | building | [specs/001-fluctlight-hub/spec.md](specs/001-fluctlight-hub/spec.md) |
