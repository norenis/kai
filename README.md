# KAI

![License](https://img.shields.io/badge/license-MIT-green)
![CLI](https://img.shields.io/badge/interface-CLI-blue)
![AI](https://img.shields.io/badge/powered_by-AI-purple)
![Go](https://img.shields.io/badge/built_with-Go-00ADD8)
![MCP](https://img.shields.io/badge/MCP-supported-black)
![Status](https://img.shields.io/badge/status-active-success)

> Your digital twin — powered by knowledge, not prompts.

---

## 🚀 Demo

<img width="800" height="400" alt="image" src="https://github.com/user-attachments/assets/474d9684-e570-48fb-9173-d42b9adebd4f" />


---

## Why KAI?

AI today is powerful — but forgetful.

- Every conversation starts from zero
- No memory of who you are
- No consistency in decisions

KAI changes that.

It gives AI a **persistent brain** that learns, remembers, and evolves with you.

---

## What it feels like

```bash
kai teach
# "I prefer TDD for business logic"
```

```bash
kai ask "How should I design this system?"
```

→ KAI answers based on what it knows about you.

---

## ⚡ Quick Start

```bash
brew tap norenis/kai && brew install kai

kai setup                          # creates ~/.kai/, generates config, registers MCP server
kai auth set-key --provider claude # save your API key
kai                                # start chatting
```

---

## 🧠 Core Concept: The Brain

```
brain/
├── identity/    # who you are
├── opinions/    # what you believe
├── style/       # how you write and code
├── decisions/   # choices you've made
└── knowledge/   # what you know
```

Brain files are markdown with YAML frontmatter. KAI appends to them as it learns — it never overwrites.

---

## 🛠 Configuration

### Config file location

kai looks for config in this order:

1. `--config <path>` flag (any command)
2. `./config.yaml` in the current directory (local/dev override)
3. `~/.kai/config.yaml` ← **created automatically by `kai setup`**

### Quick provider setup

```bash
kai setup                            # generates ~/.kai/config.yaml
kai auth set-key --provider claude   # stores key in ~/.kai/credentials.json
kai auth status                      # verify all configured providers
```

### Switching providers

Edit `~/.kai/config.yaml` and change the `provider:` line:

```yaml
provider: openai   # claude | openai | gemini | ollama
```

### Provider configuration

<details>
<summary><strong>Claude (Anthropic)</strong></summary>

```yaml
provider: claude
providers:
  claude:
    auth: api_key
    model: claude-sonnet-4-6-20250514
```

```bash
kai auth set-key --provider claude
# Paste your key from https://console.anthropic.com
```
</details>

<details>
<summary><strong>OpenAI</strong></summary>

```yaml
provider: openai
providers:
  openai:
    auth: api_key
    model: gpt-4o
```

```bash
kai auth set-key --provider openai
# Paste your key from https://platform.openai.com
```
</details>

<details>
<summary><strong>Gemini (Google)</strong></summary>

```yaml
provider: gemini
providers:
  gemini:
    auth: api_key
    model: gemini-1.5-pro
```

```bash
kai auth set-key --provider gemini
# Paste your key from https://aistudio.google.com
```
</details>

<details>
<summary><strong>Ollama (local, no API key)</strong></summary>

```yaml
provider: openai
providers:
  openai:
    base_url: http://localhost:11434/v1
    model: llama3
```

No API key needed. Start Ollama first: `ollama serve`
</details>

### Full config reference

```yaml
agent:
  name: kai           # display name shown in chat
  user_name: Kyle     # your name — used in prompts and learning

provider: claude      # active provider: claude | openai | gemini | ollama

providers:
  claude:
    auth: api_key
    model: claude-sonnet-4-6-20250514
    # base_url: https://api.anthropic.com   # optional: custom endpoint
    # timeout_sec: 120
  openai:
    auth: api_key
    model: gpt-4o
    # base_url: https://api.openai.com      # optional: change for Ollama etc.
    # timeout_sec: 120

brain:
  path: ~/.kai/brain          # where brain files are stored
  max_context_files: 5        # how many brain files to inject per query
  embeddings:
    provider: ""              # "lsi" (built-in) | "ollama" | "" (auto)
    ollama:
      url: http://localhost:11434
      model: nomic-embed-text
  sync:
    git:
      enabled: false
      remote: git@github.com:you/kai-brain.git
      branch: main

sessions_path: ~/.kai/sessions

server:
  listen_addr: 0.0.0.0:8080
  dashboard_pin: ""           # set a PIN to protect the dashboard
  sso:
    issuer: ""                # OIDC issuer URL, e.g. https://dex.example.com
    client_id: ""
    client_secret: "${OIDC_CLIENT_SECRET}"
    redirect_url: ""          # e.g. http://localhost:8080/api/auth/oidc/callback
    # scopes: [openid, profile, email]   # optional, this is the default
```

---

### 🔐 Dashboard authentication (PIN & SSO)

By default the dashboard (`kai dashboard`) is wide open to anyone who can reach the port. Two independent ways to lock it down — both can be enabled at once, and the lock screen shows whichever are configured:

- **PIN** — `server.dashboard_pin`. Simple shared secret, good for solo/local use.
- **SSO (OIDC)** — `server.sso`. Sign in through any OIDC-compliant identity provider: [Dex](https://dexidp.io/), Okta, Azure AD, Google, etc. Since Dex speaks OIDC to client apps regardless of what it federates to upstream (SAML, LDAP, GitHub, ...), pointing `issuer` at a Dex instance covers those too — kai doesn't need its own SAML support.

Setup:

1. Register kai as an OIDC client with your provider (a "static client" in Dex config) and give it a `redirect_url` of `http://<host>:<port>/api/auth/oidc/callback`.
2. Because the redirect URI must match exactly, run the dashboard on a fixed port rather than the default random one: `kai dashboard --port 8080`.
3. Fill in `server.sso` in `config.yaml`:
   ```yaml
   server:
     sso:
       issuer: https://dex.example.com
       client_id: kai-dashboard
       client_secret: "${OIDC_CLIENT_SECRET}"   # set via env var, don't commit the raw secret
       redirect_url: http://localhost:8080/api/auth/oidc/callback
   ```
4. Export `OIDC_CLIENT_SECRET` and start the dashboard. The lock screen will show a "Sign in with SSO" button in addition to (or instead of) the PIN field.

If the provider is unreachable at startup, kai logs a warning and falls back to whatever else is configured (PIN, or no auth) rather than failing to launch.

---

## 🔥 Features

- Persistent memory (brain)
- Hybrid search (BM25 + vector) with incremental indexing
- 3D sky of memories: every chunk placed by its embedding, grouped into constellations, with live effects as the brain learns
- Task management — CLI, dashboard, and MCP
- CLI-first workflow
- MCP integration with Claude Code slash commands
- Module system (GitHub PRs, Google Sheets, extensible)
- Dashboard UI: Sky, ⌘K search and cited answers, a reader with related notes, and a maintenance page (tasks, index, reorganize, import, plugins and tools, sync); no CDN at runtime
- Dashboard auth via PIN and/or OIDC SSO (Dex, Okta, Azure AD, ...)

---

## 📦 Installation

### Homebrew (macOS / Linux) — recommended

```bash
brew tap norenis/kai
brew install kai
```

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/norenis/kai/main/install.sh | bash
```

### Build from source

```bash
git clone https://github.com/norenis/kai
cd kai
make install
```

### After installation

Initialize your `~/.kai/` directory and register as an MCP server:

```bash
kai setup
```

This creates `~/.kai/brain/`, `~/.kai/sessions/`, a default `~/.kai/config.yaml`, registers kai in `~/.mcp.json` for Claude Code, and installs `/kai:*` slash commands.

---

## 🧪 Commands

### Core

| Command | Description |
|---------|-------------|
| `kai` | Start an interactive multi-turn chat session |
| `kai ask <question>` | Ask a one-shot question (no session history) |
| `kai teach` | Teach KAI something about yourself |
| `kai dashboard` | Launch the brain explorer web UI (auto-opens browser) |
| `kai version` | Print version, commit, and build date |

### Brain

| Command | Description |
|---------|-------------|
| `kai brain` | List all brain files with tags and confidence |
| `kai brain search <query>` | Full-text BM25 search across brain files |
| `kai brain edit <file>` | Open a brain file in `$EDITOR` |
| `kai brain import <files...>` | Import `.md`, `.csv`, or `.xlsx` files into brain |
| `kai brain reorganize` | Analyze brain for duplicates, miscategorizations, and small files (dry-run by default; use `--apply` to execute) |
| `kai brain reorganize dedup` | Find and merge duplicate files |
| `kai brain reorganize recategorize` | Move files to the correct category |
| `kai brain reorganize consolidate` | Merge small related files |
| `kai reindex` | Rebuild BM25 and vector search indexes manually |

**Import flags:**

```bash
kai brain import notes.md \
  --category knowledge \     # brain subdirectory (default: knowledge)
  --tags "go,testing" \      # comma-separated tags
  --confidence high \        # high | medium | low (default: medium)
  --auto-tag \               # extract tags from content automatically
  --auto-chunk               # chunk large files for better search
```

### Tasks

| Command | Description |
|---------|-------------|
| `kai task` | List today's tasks |
| `kai task add <text>` | Add a task for today |
| `kai task done <id>` | Mark a task as done |
| `kai task undone <id>` | Mark a task as not done |
| `kai task rm <id>` | Remove a task |

Tasks are stored as brain files and are also accessible from the dashboard and via MCP.

### Auth

| Command | Description |
|---------|-------------|
| `kai auth set-key --provider <name>` | Save an API key for a provider |
| `kai auth status` | Show configured vs. unconfigured providers |

### Modules (Plugins)

| Command | Description |
|---------|-------------|
| `kai module list` | List available modules and their enabled status |
| `kai module <name>` | Run a specific module (e.g. `kai module github_prs`) |
| `kai module --all` | Run all enabled modules |

Modules pull external data into the brain. Configure them in `config.yaml`:

```yaml
modules:
  - name: github_prs
    enabled: true
    config:
      token: ${GITHUB_TOKEN}
      repos:
        - owner/repo
      state: open
      limit: 20

  - name: google_sheets
    enabled: true
    config:
      api_key: ${GOOGLE_API_KEY}
      spreadsheet_id: "your-sheet-id"
      range: "Sheet1!A:Z"
      category: knowledge
      topic: my-sheet
```

Built-in modules:

| Module | Description |
|--------|-------------|
| `github_prs` | Fetch pull requests from GitHub repositories |
| `google_sheets` | Read a Google Sheet into the brain |

### Sync

| Command | Description |
|---------|-------------|
| `kai sync init` | Initialize git sync for the brain |
| `kai sync push` | Push brain changes to remote |
| `kai sync pull` | Pull brain from remote and rebuild index |
| `kai sync status` | Show local vs. remote diff |

Enable git sync in `config.yaml`:

```yaml
brain:
  sync:
    git:
      enabled: true
      remote: git@github.com:you/brain.git
      branch: main
```

### MCP

| Command | Description |
|---------|-------------|
| `kai mcp` | Start MCP server over stdio (default) |
| `kai mcp --transport http --addr :8081` | Start MCP server over HTTP (requires `KAI_MCP_TOKEN`) |
| `kai setup` | One-time setup: create brain, config, and register with Claude Code |

---

## 🔌 MCP Integration

KAI exposes all brain operations as MCP tools — meaning Claude Code, Cursor, and VS Code can read and write your brain directly.

### Claude Code (one command)

```bash
kai setup
```

That's it. Restart Claude Code and KAI appears as an MCP server.

> Use `kai setup --local` to register the installed binary instead of `go run`.

### Cursor

Add to `~/.cursor/mcp.json` (global) or `.cursor/mcp.json` (per-project):

```json
{
  "mcpServers": {
    "kai": {
      "type": "stdio",
      "command": "kai",
      "args": ["mcp"]
    }
  }
}
```

Restart Cursor. KAI tools will appear in the MCP panel.

### VS Code

VS Code MCP support is available via extensions (Cline, Continue, or GitHub Copilot with MCP enabled). Add to your extension's MCP config:

```json
{
  "mcpServers": {
    "kai": {
      "type": "stdio",
      "command": "kai",
      "args": ["mcp"]
    }
  }
}
```

Refer to your extension's documentation for the exact config file location.

### Claude Code Slash Commands

After `kai setup`, these slash commands are available in Claude Code:

| Command | Description |
|---------|-------------|
| `/kai:ask <question>` | Ask kai using brain context |
| `/kai:teach <input>` | Teach kai something new |
| `/kai:brain-list` | List all brain files |
| `/kai:brain-read <path>` | Read a brain file |
| `/kai:brain-search <query>` | Search brain by keywords |
| `/kai:brain-write` | Write or update a brain file |
| `/kai:brain-reorganize` | Analyze brain for reorganization |
| `/kai:module-list` | List available plugins |
| `/kai:module-run <name>` | Run a plugin module |

These are installed to `~/.claude/commands/kai/` automatically during setup.

---

## 📊 Performance

### Vector graph link-building

The graph uses a projection-sort sliding window (ANN) instead of all-pairs cosine comparison.

| Nodes | Old O(n²) comparisons | New O(n×W) comparisons | Speedup |
|------:|----------------------:|----------------------:|--------:|
| 100   | 4,950                 | 8,000                 | —       |
| 500   | 124,750               | 40,000                | ~3×     |
| 1,000 | 499,500               | 80,000                | ~6×     |
| 5,000 | 12,497,500            | 400,000               | ~31×    |

W = 80 (topK × 20). Node cap raised from 500 → 5,000.

### Incremental indexing

Only modified brain files are re-embedded on each run. A manifest tracks per-file hashes so unchanged files are skipped entirely.

| Brain size | Full reindex | Incremental (1 file changed) |
|-----------:|-------------:|-----------------------------:|
| 50 files   | ~8s          | ~0.2s                        |
| 200 files  | ~30s         | ~0.2s                        |
| 1,000 files| ~150s        | ~0.2s                        |

*Times are approximate and depend on embedding provider (Ollama local vs. API).*

---

## 🆚 Comparison

| Feature | kai | mem0 | Notion AI | ChatGPT Memory |
|---------|:---:|:----:|:---------:|:--------------:|
| Runs locally / offline | ✅ | ❌ | ❌ | ❌ |
| Open source | ✅ | ✅ | ❌ | ❌ |
| Structured brain files (plain markdown) | ✅ | ❌ | ❌ | ❌ |
| BM25 + vector hybrid search | ✅ | ✅ | ❌ | ❌ |
| 3D knowledge graph | ✅ | ❌ | ❌ | ❌ |
| MCP server (Claude Code, Cursor, VS Code) | ✅ | ❌ | ❌ | ❌ |
| Multi-provider LLM (Claude, OpenAI, Gemini, Ollama) | ✅ | ✅ | ❌ | ❌ |
| Git-based brain sync | ✅ | ❌ | ❌ | ❌ |
| Task management | ✅ | ❌ | ✅ | ❌ |
| External data modules (GitHub, Sheets, …) | ✅ | ❌ | ❌ | ❌ |
| No subscription required | ✅ | ❌ | ❌ | ❌ |

---

## 🧬 Philosophy

> AI should remember who you are.

---

## 📄 License

MIT
