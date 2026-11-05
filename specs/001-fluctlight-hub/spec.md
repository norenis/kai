# Fluctlight Hub - dashboard redesign ("Celestial Orrery")

- Status: draft, awaiting review
- Date: 2026-10-24
- Scope: `internal/dashboard` (UI + new endpoints), `internal/brain` and `internal/retriever` (instrumentation), new `internal/events`
- Approved visual reference: a hi-fi HTML mock kept **outside this repo** (it is built from a real private brain). Every visual decision below is taken from that mock; when this document and the mock disagree, the mock wins.

## 1. Goal

Turn the dashboard from a generic admin panel into a place where you can *see the brain*. The vector space is the centrepiece and should feel like a living soul (a "Fluctlight", in the sense of Sword Art Online: Alicization) that visibly takes in and gives out memories.

The owner uses the dashboard for four things, in this order of weight:

1. See what the brain knows and what it learned recently.
2. Find and read notes.
3. Ask the brain (needs an LLM provider; see Non-goals).
4. Maintain it (tasks, reindex, decay, reorganize, import, plugins, tools, sync).

Success means:

- The home screen is the 3D vector space, with real memories placed by their embeddings and real events animated as they happen.
- Text on screen is minimal; it should feel like a real app, not a movie poster.
- Every capability of the current dashboard is still reachable (nothing is removed, only regrouped).
- It runs smoothly (target 60 fps, floor 40 fps) with ~3-5k chunks on a modest older desktop (integrated graphics) in Chrome/Safari.

## 2. Non-goals

- Enabling an LLM provider for any brain. The "ask" flow is designed and built, but stays in its disabled state until a provider is configured. Deciding that is out of scope.
- Editing notes in the UI (the dashboard stays read-only for note content, as today).
- Changing auth (PIN / OIDC) behaviour.
- Mobile-first design. Small screens must work (see 7.6) but are not the primary target.

## 3. Design language

Name: **Celestial Orrery** - a golden armillary sphere (a 3D astrolabe) around a holographic cyan soul. It combines two references: the Prometheus "orrery" hologram (volumetric light, depth, bloom) and an astrolabe / antique star chart (hairline gold, fine degree ticks, constellations, serif names).

### 3.1 Tokens

| Token | Value | Use |
|---|---|---|
| `--void` | `#03060b` → `#11233b` radial | page and scene background (navy, faint star dust) |
| `--gold` | `#d8bc84` | armillary rings, hairlines, numerals, accents |
| `--champagne` | `#f3dfae` | fresh memories, inflow ribbons, constellation stars |
| `--soul` | `#6fe3f5` / `#c8f6ff` | hologram core particles, filaments |
| `--ink` | `#eef1f2` / `#a7b0b8` / `#6b7680` | text primary / secondary / tertiary |
| `--coral` | `#ff6f5c` | the only alert colour: locked target, "resting N days", anomalies |
| hairline | `rgba(216,188,132,.2-.55)` 1px | separators; no boxed cards except the two sheets (7.2, 7.3) |

Rules: no em-dash in UI copy; coral is never decorative; gold is never used for errors.

### 3.2 Typography

- **Cormorant Garamond** (italic 500): names and titles - brain name, note titles, constellation names, tabs, buttons-as-links.
- **Montserrat** (300/400): numbers and small caps labels (letter-spacing .26-.36em, uppercase, 8.5-9.5px).
- **Inter** (400): reading text (note body, search snippets, answers), 14.5px / 1.75.
- Fonts are self-hosted (woff2 under `static/fonts`), not loaded from Google at runtime.

### 3.3 Motion principles

- Everything eases (cubic in-out); nothing snaps. Camera moves glide over ~1s.
- Data flows along **curves**, never straight lines; flows are continuous streams of small particles, never discrete "shots".
- Idle state is calm: slow auto-rotation (0.32 deg/s equivalent of OrbitControls `autoRotateSpeed`), rings drift, soul particles twinkle softly.
- `prefers-reduced-motion`: no auto-rotation, no flows; events show as a brief static glow.

## 4. Information architecture

A slim left rail with three destinations: **Sky** (home), **Read**, **Sys** (maintenance). Search/ask is a global sheet opened with `⌘K` or `/` from anywhere.

| Old page | New home |
|---|---|
| Overview | Sky (identity + inflow) and Sys → Overview tab |
| Vectors | Sky (the whole scene) |
| Search, Chat | `⌘K` sheet (type = search, `⌘↵` = ask) |
| Files | Read drawer (list + note) |
| Tasks, Import, Plugins, Tools, Reorganize, Sync, decay/migrate/reindex | Sys tabs |

## 5. The scene (three.js)

Pinned three.js `0.169.x`, vendored under `static/vendor/` (no runtime CDN). One `WebGLRenderer` + `EffectComposer` (RenderPass → UnrealBloomPass(strength .7, radius .45, threshold .18) → OutputPass). The scene persists across all screens; screens only move the camera and change highlight state.

### 5.1 Components

1. **Soul** - one `Points` with a custom shader (round soft sprite, additive blending, per-point `size`, `bright`, `phase` for twinkle). One point per chunk.
   - Position: server PCA-3D coordinates, re-centred, then compressed radially into a sphere: `r' = (1 - e^(-r/s)) * 1.22`, `s` = 45th percentile of radii. Direction is preserved, so semantic neighbours stay neighbours.
   - Colour: champagne if the note was updated within ~7 days, otherwise soul cyan (35% of points use the lighter `#c8f6ff` for texture).
   - Brightness: `max(.72, 1 - ageDays/25)` for notes updated in the last 30 days, `.42` otherwise.
   - Size: `.03 + bright * .045` (world units, attenuated by distance). Read count may later scale size; not required now.
2. **Filaments** - `LineSegments` for the strongest ~1500 similarity links, cyan, opacity .06.
3. **Constellations** - for each cluster (6.3): take ~11 bright member chunks, connect them with a minimum spanning tree, draw in gold (opacity .55) with brighter "star" points at the vertices.
4. **Armillary sphere** (radius 1.5): equator band with 360 fine ticks and 72 major ticks, meridian band with ticks, colure band, ecliptic band tilted 23.4° with ticks, polar axis with two small finials. Bands are thin tori (`radius .002-.006`). Equator and ecliptic rotate slowly at different speeds.
5. **Dotted shell** - ~1600 Fibonacci-sphere dots just inside the armillary, faint gold.
6. **Orbits** - three thin tilted rings outside the armillary, each carrying a small "planet" for one of the three largest clusters.
7. **Base** - an engraved disc below the sphere (concentric rings, 360 ticks, 36 major ticks) and a faint upward light cone.
8. **Lock ring** - coral ring that faces the camera and gently pulses around a focused memory.

### 5.2 HTML overlays projected from 3D

Each frame, project 3D anchors to screen and position absolutely-placed HTML:

- Constellation labels: italic serif name (from cluster label, title-cased, joined with " & ") and a small-caps line `<Roman numeral> · <n> memories`. Anchor = constellation centroid pushed outward by .55 and lifted above its top star. Labels facing away from the camera fade to 30%.
- Roman numerals I-IX orbiting just outside the equator (one per cluster, ordered by size).
- Lock callout next to the focused memory (coral small caps + serif title + one meta line).
- Home shows labels for the 3 largest clusters only; search/ask shows only clusters that contain hits; Read hides them. Label collision handling: if two labels overlap on screen, hide the smaller cluster's label for that frame.

### 5.3 Camera per screen

| Screen | Target | Distance | Dim others |
|---|---|---|---|
| Sky | `(0, -.1, 0)` | 6.9 | 0 |
| Search | `(0, -2.25, 0)` (sphere rises above the sheet) | 12.5 | .65 |
| Ask | `(0, -1.85, 0)` | 12 | .65 |
| Read list | `(1.55, -.1, 0)` (sphere shifts left of the drawer) | 7.2 | .35 |
| Read note | `(1.35, -.15, 0)` | 7.6 | .5 |
| Sys | scene shrinks to a 288x180 live thumbnail top-right (CSS transform on the canvas) | - | 0 |

User can always drag to orbit; zoom and pan are disabled. Auto-rotation resumes after 30 s idle.

### 5.4 Event effects

| Event (6.1) | Effect |
|---|---|
| `learn` / `create` | **Inflow ribbon**: ~220 particles sharing one cubic Bézier path from a random point on a radius-4.6 sphere, swinging tangentially around the orrery and curving into the target chunk; head bright and large, tail thinning (`size ∝ (1-g)^1.4`), ~5 s with cubic ease. On arrival: target swells, a thin champagne ripple ring expands and fades (~1.5 s), and linked chunks brighten in two hops (1-hop +.9, 2-hop +.35) then decay (×.975 per frame). The matching Inflow row glows. If the chunk is not yet in the vector set (not reindexed), the ribbon ends at the core; the point appears on the next vectors refresh (debounced 10 s). |
| `update` | Same ribbon, cyan-white, shorter (~3 s), no ripple. |
| `delete` | Target points fade out over 1.5 s and drift outward as dust. |
| `search` / `ask` | Hit chunks breathe (staggered sine, `1.15 + .45 sin(1.8t + i·.37)`), others dim; a **continuous stream of fine particles** flows along curves from hit chunks to the top edge of the open sheet (spawn ~1 particle/frame, life ~3-5 s, alpha `sin(π t)`). Stops when the sheet closes. |
| `touch` (read) | Brief soft glow on the chunk; no flow. |

Performance: flows use a single preallocated `Points` buffer (1400 particles); per-frame CPU work is O(active particles + chunks). Rendering pauses when the tab is hidden.

## 6. Data and backend

### 6.1 Event bus - `internal/events` (new)

```go
type Event struct {
    Type   string    `json:"type"`   // learn|create|update|delete|search|ask|touch
    Path   string    `json:"path,omitempty"`
    Chunks []string  `json:"chunks,omitempty"` // chunk ids "path#n"
    Query  string    `json:"query,omitempty"`
    Trace  *Trace    `json:"trace,omitempty"`  // search/ask only
    Source string    `json:"source"`           // mcp|dashboard|cli|fs
    TS     time.Time `json:"ts"`
}
type Trace struct { Stages []Stage `json:"stages"`; TotalMS float64 `json:"total_ms"` }
type Stage struct { Name string `json:"name"`; MS float64 `json:"ms"`; Count int `json:"count"` }
```

- `Bus`: `Publish(Event)`, `Subscribe(ctx) <-chan Event`, ring buffer of the last 200 events (`Recent()`); slow subscribers are dropped, never block publishers.
- Emit points (in-process): `FileBrain.Save`, `Learn`, delete paths, `Touch`; retriever search and the chat/ask path. The MCP server and dashboard share one process (`internal/mcp/server.go` mounts the dashboard), so one bus instance is passed to both.
- Out-of-process writes (`kai task` via CLI, `kai brain …`, git sync) are caught by an **fsnotify** watcher on the brain directory. It ignores `index.json`, `vectors.bin.gz`, `.trash/`, temp files, and any path the bus itself published within the last 2 s (dedupe by path).

### 6.2 Endpoints

| Endpoint | Change |
|---|---|
| `GET /api/events` (new) | SSE behind the existing auth middleware. Sends `Recent()` first, then streams. Heartbeat comment every 25 s. `retry: 3000`. |
| `GET /api/vectors` | Add per node: `title`, `updated`, `views`, `words`, `cluster`. Add top-level `clusters: [{id, label, size, notes, numeral}]` sorted by size. |
| `GET /api/search` | Add per result: `title`, `cluster`. Add `trace` (stage timings and counts: embed, bm25, vector, prf, fuse). **Fix**: snippets are cut by bytes and can split a UTF-8 rune (shows `U+FFFD`); cut on rune boundaries and prefer word boundaries. |
| `GET /api/files`, `GET /api/file` | Add `title` (first `# ` heading, falling back to a title-cased filename). |
| `GET /api/related?path=` (new) | Top 5 notes by summed link strength to the note's chunks (vector links), with `title`, `cluster`, `score`. |
| `GET /api/clusters` | Not needed; clusters ride on `/api/vectors`. |

### 6.3 Clusters ("constellations")

- Computed server-side on the **full embedding vectors** (Gemini 1536D or LSI), not on the 3D PCA projection. The mock clustered on PCA-3D and produced visibly wrong groupings (unrelated notes merged), so this is a hard requirement.
- k-means++ with `k = clamp(round(sqrt(nChunks/40)), 5, 12)`, cosine distance, fixed seed, best of 5 restarts.
- Label: the two most distinctive tags of the cluster (tag frequency in the cluster × `log(1 + nNotes/(1 + globalTagNotes))`), skipping a small stop-list of generic tags; fall back to the dominant category. Numerals I.. by size.
- Cached with the vector index; recomputed on reindex or when the chunk count changes by more than 5%.

## 7. Screens

### 7.1 Sky (home)

- Top-left identity, no frame: small caps `FLUCTLIGHT OF`, brain name in large italic serif, `<n> memories · <k> constellations`, coral status line (`RESTING · LEARNED 5 DAYS AGO` when the last write is older than 48 h; otherwise gold `LEARNING · <relative>`), gold hairline.
- Top-right **Inflow**: last 3 events, each = italic serif note title + small caps `<verb> · <relative time>`. Hover locates the memory (lock ring), click opens it in Read.
- Bottom-centre query line: underline only, italic serif placeholder `Ask <name> what it remembers…`, `⌘ K` hint.
- Bottom-right: tiny `drag to turn the sphere` + live RA readout.
- Interactions: hover a memory → lock ring + callout; click → Read note; drag → orbit.

### 7.2 `⌘K` sheet - search

- Rounded dark glass sheet (radius 18, gold hairline border, blur 18px), bottom-centre, 760px.
- Input row: circle glyph, query in italic serif, right side small caps `<n> memories · <m> notes · <numerals>`.
- **Recall trace** row: gold dots linked by hairlines: `EMBED 118ms - BM25 3ms - VECTOR 6ms - PRF 4ms - FUSE 1ms`, lighting up in sequence. Values come from `trace`.
- Results (max 4 visible, scroll for more): numeral of the cluster, italic serif title, 2-line snippet (Inter) with matched terms underlined in gold, score with a thin gold bar.
- Footer: `↑↓ move`, `↵ read`, `esc close`, and `⌘↵ ask` (disabled with "no llm configured" when the brain has no LLM).

### 7.3 `⌘K` sheet - ask

- Same sheet. Trace shows `EMBED → RECALL → SYNTHESIZE`. Answer streams (Inter) with Roman-numeral superscript citations; sources listed below as `i  <title>` in italic serif. Follow-up questions stay in the same sheet; `esc` collapses and keeps history for the session.

### 7.4 Read - list

- Right drawer, 560px, dark glass. Header `Read` (italic serif 34px) + `310 notes`. Underline filter field (`Filter by name, tag or constellation…`), sort, constellation chips (italic serif, active = gold underline).
- Grouped by category. Row: numeral · italic serif title · small caps first 3 tags · right side relative date + word count. Hover highlights the note's chunks (gold) in the sphere.

### 7.5 Read - note

- Breadcrumb (small caps): `← READ · <category> · <numeral> <constellation>`.
- Title in italic serif 34px; meta line (confidence, source, relative date, fragments, words, reads); tags in italic serif.
- Body: rendered markdown (Inter 14.5/1.75), `##` headings in italic serif champagne, gold list markers, copy button on code blocks.
- Footer **Nearest in the sky**: 5 related notes (numeral, title, gold strength bar) from `/api/related`.
- Camera focuses the note: its chunks gold, related notes' chunks champagne, the rest dimmed. `vault` notes keep the existing PIN gate.

### 7.6 Sys (maintenance)

- Plain page, no boxes. Header `Maintenance` (italic serif 46px) + small caps summary. Tabs as italic serif links: Overview, Tasks, Index, Reorganize, Import, Plugins & tools, Sync.
- **Overview** tab (three columns + two):
  - Constellations: numeral, name, count, hairline bar.
  - Learning · 14 days: one glowing dot per day sized by notes learned; coral `quiet since <date>` when applicable.
  - Index: memories embedded, links, dims · provider, BM25 health; actions `Rebuild embeddings`, `Reindex`, `Decay · dry run` as gold underlined links.
  - Recent activity table (hairline rows); anomalies in coral (e.g. a task file far larger than usual, flagged as `unusual`).
  - Most used tags.
- Other tabs keep their current functionality, restyled. Destructive or long actions (decay for real, migrate, reorganize apply) require confirmation and show progress; the live thumbnail reacts to their events.
- Small screens (< 900px): rail becomes a bottom bar, drawers become full-screen sheets, the scene renders at reduced point size and no bloom.

## 8. Frontend structure

- `templates/index.html`: shell (rail, overlay containers, sheets, drawer, sys page), import map pointing to vendored modules.
- `static/`: `app.js` (state, routing between screens, data loading, SSE client with reconnect), `scene.js` (three.js scene, camera views, highlight API, projections), `flows.js` (particle flows and event effects), `cmdk.js`, `reader.js`, `sys.js`, `theme.css`, `fonts/`, `vendor/` (three + addons, marked, Vue if kept).
- Drop Tailwind CDN, chart.js and 3d-force-graph. Small charts are SVG/CSS.
- All embedded with `go:embed` (extend the existing `templates/*` pattern to `static/*`), no build step.
- Scene API used by the rest of the app: `setView(screen)`, `highlight(chunkIds, {color, dim})`, `related(chunkIds)`, `focus(chunkId)`, `onEvent(event)`, `setVectors(payload)`.
- WebGL unavailable → fall back to a static 2D canvas render of the same points (no bloom, no flows); everything else works.

## 9. Testing

- Go: unit tests for the bus (ordering, ring buffer, slow subscriber drop), fsnotify dedupe, SSE handler (initial replay + heartbeat), cluster labelling, snippet rune-safe truncation, `/api/related`. Use a real temporary brain directory (repo convention: no mocks).
- Playwright e2e (`tests/e2e`): scene canvas renders and is non-blank; `⌘K` returns results and opens a note; Read list filter; Sys tabs render; writing a note through the API produces an Inflow row within 3 s via SSE.
- Visual: screenshot each screen and compare side by side with the approved mock before merging.
- Performance: measure fps on a modest older desktop with the largest brain (~3.2k chunks); must stay ≥ 40 fps on Sky with bloom.

## 10. Rollout

1. Branch from the current dashboard work (see Open items about uncommitted OIDC changes).
2. Build `kai:1.2.0-fluctlight1` for the private registry (Dockerfile builds Go inside the image).
3. Deploy to the least important instance first, verify, then the others. Keep a backup of each compose file for instant rollback to the previous tag.

## 11. Open items

- Uncommitted, staged OIDC dashboard work exists in the working tree; it must be committed (or deliberately excluded) before this branch is cut.
- LLM provider for "ask": deferred by the owner.
- A daily task file was observed at ~40k words (expected: a few dozen lines). Likely a sync bug outside this project; investigate separately. The Sys overview will surface such anomalies but does not fix them.
- Cluster labels from tags may read oddly for brains with sparse tags; acceptable for v1, revisit after seeing real output on full vectors.
