# Design

The UI source of truth for the kai dashboard ("Fluctlight Hub", Celestial Orrery theme). Read only the sections your change needs (`grep -n '^#' DESIGN.md`). A change that adds or changes a token, component or pattern updates this file in the same commit. The full original design is in `specs/001-fluctlight-hub/spec.md` (sections 3 to 5 cover language, information architecture and the scene).

## Principles
A dark, calm scene: a golden armillary sphere around a holographic "soul" built from real brain chunks, with a global search/ask sheet. Real data and text only; the demo brain in `tests/fixtures` is synthetic. No invented illustrations. Respect reduced motion and small screens.

## Tokens
Defined in `internal/dashboard/static/theme.css` (`:root`):
- Color: `--void #03060b`, `--void-hi #11233b`, `--gold #d8bc84` (`--gold-2`, `--gold-3` alpha), `--champagne #f3dfae`, `--soul #6fe3f5`, `--soul-hi #c8f6ff`, `--ink #eef1f2`, `--ink-2 #a7b0b8`, `--ink-3 #6b7680`, `--coral #ff6f5c`.
- Type: `--serif` Cormorant Garamond, `--sans` Montserrat, `--body` Inter (fonts vendored in `static/fonts`).
- Layout: `--rail 64px` (side rail). Spacing, radius, shadows: UNCONFIRMED, not tokenized; read `theme.css`.

## Components
| Component | Path | Variants / states | Notes |
|---|---|---|---|
| Scene (three.js) | `internal/dashboard/static/scene.js`, `points.js` | per-screen camera | persistent; vendored three.js in `static/vendor`; no-WebGL fallback tested in `scene-nogl.spec.ts` |
| Labels | `static/labels.js` | collision avoidance | HTML overlays projected from 3D |
| Flows / Inflow | `static/flows.js`, `static/inflow.js` | live event arrival effects | driven by `/api/events` |
| Command sheet | `static/cmdk.js` | search, ask (streamed, cited) | opens with the command-K shortcut |
| Reader | `static/reader.js`, `markdown.js` | list drawer, note view | |
| Sys page | `static/sys.js`, `sys-more.js`, `sys-actions.js` | Overview, Tasks, Index, Reorganize tabs | |
| App shell | `static/app.js`, `templates/index.html`, `templates/lock.html` | lock screen for the vault | |

## Layout and screens
Left rail (`--rail`), full-bleed scene, screens: Sky (home), Read, Sys, plus the command sheet overlay. Information architecture: spec section 4.

## Patterns
Dark theme only. Loading and live updates arrive through SSE events. Reduced-motion and small-screen behavior is covered by `tests/e2e/small.spec.ts`. Forms, error and empty states: UNCONFIRMED, not documented yet.

## Accessibility
UNCONFIRMED: no written contrast targets. Keyboard: command-K sheet closes and blurs its input. Checked only by the Playwright specs in `tests/e2e`.
