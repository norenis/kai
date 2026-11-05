# kai - agent rules

**kai** is a CLI-based personal AI knowledge system ("digital twin") written in Go. It learns about the user through conversations, stores knowledge in structured markdown brain files, and exposes it through a CLI, a web dashboard and an MCP server. It is a public repository: keep everything tracked neutral (no real brain data, no private host names, no home paths).

## Project profile
- **Build / run:** `make build` (binary `./kai`, version ldflags), `make install`, `make clean`. Dashboard: `./kai dashboard` (default port 8080; the e2e suite uses 9234). Config from `config.yaml.example`.
- **Test (narrow -> full):** `go test -run TestName ./...`, `go test ./internal/router/... -v`, `make test` (`go test ./... -v`); e2e: `pnpm exec playwright test` (needs `./kai dashboard` on port 9234, see `tests/e2e/serve-demo.sh`). CI (`.github/workflows/verify-build.yml`) runs `go test -race -coverprofile` and a build on push/PR to `main`.
- **Lint / typecheck:** `go vet ./...`; `gofmt`. Pre-commit leak check: `python3 -I tasks/private-terms-check.py` (local-only script, `tasks/` is git-excluded).
- **Heavy commands:** Playwright e2e, `go test -race ./...`, Docker-based Go runs; on the shared dev machine run them through `~/.local/bin/run-idle`.
- **Debug flow:** none (agent-skills `debugging-and-error-recovery`).
- **Deploy & production:** releases are built by GitHub Actions (`.github/workflows/build-release.yml`) when a `v*` tag is pushed; `update-homebrew-tap.yml` updates the Homebrew formula. Tags and pushes are the owner's call only; agents never push or tag.
- **Data & secrets:** brain files under `~/.kai/brain/` (configurable `brain.path`), API keys in `~/.kai/credentials.json` (managed by `kai auth set-key`), provider/OIDC settings in `config.yaml`. Tests use a synthetic demo brain (`tests/fixtures`), never a real brain. Never read or print credentials.
- **UI:** dashboard in `internal/dashboard` (Go templates + plain ES modules, no build step, embedded via `go:embed`; three.js vendored under `static/vendor`). Playwright tests in `tests/e2e`.
- **Git flow:** `GITFLOW.md`
- **Read first:** `ARCHITECTURE.md` (package map and data flow before adding a package or provider); `DESIGN.md` for dashboard work.
- **Handoff:** `private/handoff/INDEX.md` (local only, `private/` is git-excluded via `.git/info/exclude`), maintained by the shared `handoff` and `resume-handoff` skills; the kai-only steps are under "Handoff extras" and "Resume extras" below.
- **Workflow options:** parallel task agents: 1; per-task review: off
- **Language:** English in the repo (code, comments, UI, commits). Some older commit subjects are Vietnamese; do not repeat that.

## Project rules
- **No mocks for integration tests:** tests in `router` and `brain` use real file I/O.
- **Streaming responses:** all providers stream; never buffer a full response.
- **Brain files are append-friendly:** learning extraction adds to existing files, never overwrites.
- **MCP tools mirror CLI:** every brain operation in the CLI must also be an MCP tool.
- **Config is loaded once:** pass `*config.Config` through; do not reload mid-request.
- **Provider interface** is `internal/provider/types.go` (`Chat`, `Stream`); use `ApplyOptions(opts)` inside implementations.
- **Adding a provider:** create `internal/provider/<name>.go`; register via `Registry.Register(name, provider)` (see `buildRouter` in `internal/cli/`); add config under `Providers` in `internal/config/config.go`; update `config.yaml.example`; test with `httptest.NewServer` (`go test ./internal/provider/...`).
- **Adding a CLI command:** `internal/cli/<name>.go` with an unexported `new<Name>Cmd()` returning `*cobra.Command`, no parameters; call `loadConfig()` at the top of `RunE`, then `buildRouter(cfg)` if LLM access is needed; register in `internal/cli/root.go` with `root.AddCommand(...)`. Only `NewRootCmd()` is exported.
- **Brain format:** markdown with YAML frontmatter (`tags`, `confidence`, `source`, `updated`) in `identity/`, `opinions/`, `style/`, `decisions/`, `knowledge/`; indexes `index.json` (BM25) and `vectors.bin.gz`.
- **Config:** `config.yaml`; providers `claude`, `openai`, `gemini`, `ollama`; embeddings `lsi` (built-in) or `ollama`.
- **Unit tests** sit beside source as `*_test.go`; commits are signed.
- Go may be missing from a non-login shell PATH on the dev machine: use a login shell, or the Docker Go image named in `private/specs/001-fluctlight-hub/plan.md`.

## Handoff and resume extras

### Handoff extras
Done on top of the shared `handoff` skill. The kai handoff stays local: `private/handoff/` is untracked on purpose, never stage or commit it.
- Task status: count `- [x]` and `- [ ]` in `private/specs/001-fluctlight-hub/tasks.md`, list every open box with its note, and say which checkpoint in `private/specs/001-fluctlight-hub/plan.md` was last approved.
- Build and tests: `make build`, `go vet ./...`, `make test` (Docker Go image from the plan if `go` is not on PATH); record the e2e result if dashboard code changed.
- Before any commit: `python3 -I tasks/private-terms-check.py` passes on the staged changes (public repo, no real brain data).
- Leftovers: stray dashboard servers (`pgrep -fl "kai dashboard"`, ports 9234 and 8080), `/tmp` scripts the next session needs, containers started by tests.
- Next steps name the task id in `private/specs/001-fluctlight-hub/tasks.md`; mark steps that need the owner (push, tag, deploy).

### Resume extras
Done on top of the shared `resume-handoff` skill:
- Expected branch: the current feature branch of the open release (see `GITFLOW.md`), today `release/albatross/001-fluctlight-hub`.
- Read `private/specs/001-fluctlight-hub/plan.md` (commands, checkpoints, risks) and the open boxes in its `tasks.md`; read `specs/001-fluctlight-hub/spec.md` only for the sections the next task touches.
- Run `make build && make test` and `go vet ./...` before continuing; check the leftovers listed in the handoff, and do not kill processes you did not start.
- Tick a task box only with evidence and note how it was verified. Never push, tag or deploy without the owner's OK in the conversation.
