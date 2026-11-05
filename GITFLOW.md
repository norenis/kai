# Git flow

How this repo uses git. Agents follow it for branches, commits, merges and tags. It can only add restrictions: pushing, merging into `main` or a release main, and creating deploy tags always need the owner's OK in the conversation, whatever this file says.

## Model: release-train (target; not yet in use)
| Branch | Name | Created from | Merges into | Rules |
|---|---|---|---|---|
| Main | `main` | - | - | latest released code; no direct commits |
| Release main | `release/<release>/main` | `main` | `main` when the release ships | no direct commits |
| Feature | `release/<release>/<NNN-feature>` | its release main | its release main | one feature per branch; matches `specs/NNN-<feature>/` |
| Integration | `release/<release>/integration` | its release main | nothing | throwaway; never merged back |
| Release fix | `release/<release>/fix/<issue>` | release main or feature branch | the branch it came from | bugs before the release ships |
| Hotfix | `hotfix/<issue>` | commit of the latest production tag | `main` and open release mains | production bugs only |
| Request | `request/<request>` | `main` or an open release main | where it came from | change asked for outside a planned release |

Names: lowercase kebab-case, ASCII. Never create a bare `release/<release>` branch. Delete feature, fix, hotfix, request and integration branches after merge or drop. Worktrees: none in use.

## Current branches (switch to the release-train not yet scheduled)
- `main` (origin/main): released code, tags `v1.0.2`, `v1.1.0`, `v1.2.0` and earlier.
- `feat/fluctlight-hub`: local only, never pushed (the public repo takes no real brain data); dashboard redesign, spec in `specs/001-fluctlight-hub/`, plan kept local in `private/`.
- `feat/incremental-indexing`: also on origin.
- `feat/dashboard-oidc-sso`: local.

Steps to switch (not executed): 1) the first release is `albatross`; 2) create `release/<release>/main` from `main`; 3) rename or recreate `feat/fluctlight-hub` as `release/<release>/001-fluctlight-hub` (numbers follow `specs/NNN`) and the other two `feat/*` likewise; 4) retarget merges to the release main; 5) ship by merging the release main into `main`, then tag.

## Release names
Birds, in this order (agreed 2026-11-05); the next release takes the next unused name; after the last, restart with `-2`. Skip a name equal to the product name.

albatross, bittern, curlew, dunlin, egret, falcon, godwit, heron, ibis, jay, kestrel, lapwing, merlin, nightjar, osprey

| Release | Release main created | Status | Features |
|---|---|---|---|
| andromeda (proposal) | not created | - | fluctlight-hub, incremental-indexing, dashboard-oidc-sso |

## Deploy tags
- Existing releases use `v*` tags (`v1.2.0`), which trigger the GitHub Actions release build. Keep them for public releases.
- Environment deploy marks, if used: `dev/<yyyy.MM.dd.hhmm>-<release>` and `prod/<yyyy.MM.dd.hhmm>-<release>` on the exact commit. Only the `devops` agent creates or pushes tags, and only with the owner's quoted approval. No deploy branches.

## Commits
- **Format:** `area: lowercase summary` (examples: `dashboard: Sys Tasks, Index and Reorganize tabs`, `brain: fix the incremental reindex deadlock`, `docs: update README`). English only.
- **Signing:** required (`commit.gpgsign=true`, ssh key); never bypass.
- **Trailers:** none; no Co-Authored-By or other AI attribution.
- **Scope:** one topic per commit; stage files by path; never commit secrets, real brain data, `.env`, `tasks/`, `private/` or `.claude/settings.local.json`.

## Integration
- **How branches merge:** local merge commit or PR on GitHub; merging into `main` is the owner's call.
- **Before merging:** `go vet ./...`, `make test`, `pnpm exec playwright test` for dashboard changes; CI job `verify-build` (`go test -race`, build) green; `python3 -I tasks/private-terms-check.py` passes.
- **Hooks:** no git hooks installed (`.git/hooks/pre-commit` absent); the private-terms check is run by hand.

## Remote
- **Remotes:** `origin` is the public GitHub repo `norenis/kai`.
- **Push:** only with the owner's OK for that push; never force-push `main` or a release main. The repo is public: nothing private leaves the machine.
