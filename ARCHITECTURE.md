# Architecture

Read this before adding a package, module, helper or test fixture. Keep it current: a change that adds, moves or removes one of the things below updates this file in the same commit.

## Overview
kai stores personal knowledge as markdown "brain" files, indexes them (BM25 plus vectors) and answers questions through a retrieval-augmented pipeline. It is exposed as a CLI, a web dashboard and an MCP server.

Data flow: `User -> Router -> Retriever (brain search) -> Prompt Builder -> Provider (LLM) -> Response + Learning Extraction -> Brain update`

## Components
| Component | Path | Responsibility | Talks to |
|---|---|---|---|
| Entry point | `cmd/kai/main.go` | calls `cli.Execute()` | cli |
| CLI | `internal/cli/` | Cobra subcommands (root, ask, teach, brain, mcp, dashboard, sync, auth, setup, reindex) | everything below |
| Router | `internal/router/` | pipeline: retrieval, prompt, LLM, learning extraction | retriever, prompt, provider, brain |
| Brain | `internal/brain/` | file storage, BM25 index, vector embeddings, chunking, vault category | disk |
| Retriever | `internal/retriever/` | hybrid search (BM25 + vector) | brain |
| Prompt | `internal/prompt/` | system/user prompt construction with brain context | router |
| Provider | `internal/provider/` | LLM integrations: claude, openai, gemini (interface in `types.go`) | external APIs |
| Config | `internal/config/` | YAML loader (`config.yaml`) | disk |
| Auth | `internal/auth/` | API key management (`~/.kai/credentials.json`) | disk |
| Session | `internal/session/` | multi-turn conversation history | router |
| MCP | `internal/mcp/` | MCP server (stdio and HTTP) | router, brain |
| Dashboard | `internal/dashboard/` | web UI: chat, brain explorer, Sky scene, Read view, Sys tabs, OIDC login, SSE events | router, brain, events |
| Events | `internal/events/` | in-process brain event bus feeding `/api/events` | brain, dashboard |
| Module | `internal/module/` | UNCONFIRMED: module/feature plumbing, not described in earlier docs | cli |
| Sync | `internal/sync/` | git-based brain sync | git |

## Data
Brain files in `~/.kai/brain/` (configurable): markdown with YAML frontmatter; folders `identity/ opinions/ style/ decisions/ knowledge/`; `index.json` (BM25 inverted index), `vectors.bin.gz` (embeddings). Credentials in `~/.kai/credentials.json`. No database.

## Interfaces
- CLI (`kai <subcommand>`), MCP tools mirroring CLI brain operations, dashboard HTTP API (`/api/...`, SSE `/api/events`, `/api/related`, `/api/tasks`), config file `config.yaml` (example: `config.yaml.example`). Provider contract: `internal/provider/types.go`.

## Shared helpers and test fixtures
- `tests/fixtures/`: synthetic demo brain for e2e and dashboard tests (never real data).
- `tests/e2e/serve-demo.sh`: starts the dashboard on the demo brain for Playwright.
- `ApplyOptions(opts)` in `internal/provider/`: resolves call options for providers.
- `loadConfig()` / `buildRouter(cfg)` in `internal/cli/`: config and router wiring for commands.

## Runtime and deployment
Single Go binary `kai`; dashboard on port 8080 by default. Release: pushing a `v*` tag triggers GitHub Actions (`build-release.yml`, darwin/linux, amd64/arm64) and the Homebrew tap update; `Formula/` and `install.sh` are the install paths; `Dockerfile` and `docker-compose.yaml` exist for container runs.

## Decisions and constraints
No mocks for integration tests; streaming only; brain files append-friendly; MCP mirrors CLI; config loaded once (see AGENTS.md rules). Dashboard has no build step (ES modules + import map, assets embedded). `docs/decisions.md` does not exist yet.

## Known duplication / debt
Open dashboard work is tracked in `private/specs/001-fluctlight-hub/tasks.md` (local, git-excluded). The repo is public, so `private/` content never ships.
