# web/ — Frontend Conventions

Auto-loaded when working in `web/`. Backend conventions live in the root
`CLAUDE.md`.

## Conventions

- Functional components + hooks/context only. No class components, no Redux.
- API calls go through the typed wrapper in `web/src/api/client.ts` — every
  endpoint in one file.
- Styling is Tailwind utility classes + CSS custom properties only. No CSS
  modules, no styled-components, no hardcoded hex (see Color palettes).
- Files: `PascalCase.tsx` components, `useX.ts` hooks, `types/index.ts` shared
  types. Split components over ~150 lines.
- **One SSE connection.** `SSEProvider` (`web/src/hooks/useSSEBus.tsx`) owns the
  single `EventSource('/api/events')`; consumers call `useSSEBus()` and register
  handlers via `subscribe(onEvent)` in a `useEffect` (the return value
  unsubscribes). Reconnect/backoff lives in the provider. Never open a second
  `EventSource` to that origin — Firefox aborts concurrent same-origin SSE with
  `NS_BINDING_ABORTED` (`docs/gotchas.md`).
- Theme lives in `ThemeProvider` (`web/src/hooks/useTheme.ts`); consume via
  `useTheme()`. Don't add a parallel theme mechanism.
- `vite.config.ts` proxies `/api` → `http://localhost:8080` for dev.
- **No `localStorage` outside the documented allowlist:** theme, `palette`, human
  agent ID, last project, `last_chat_id`, chat section collapse, `chat_layout`,
  collapsed column/card state, `chat_filter_prefs`, rail-expanded, NowRail-open.
  Adding a key means adding it here.

## Color palettes

Three palettes — **Everforest** (default), **Radix**, **Catppuccin** — defined
entirely in `web/src/index.css`. All three set the _same_ CSS custom properties,
so components reference variables only and need no changes when the palette
switches.

**Selection:** the server default comes from `theme` in config
(`GET /api/app/config`); users override it per-browser via the PaletteSelector in
`AppHeader`, persisted to `localStorage.palette`. `ThemeProvider` applies
`data-palette="<name>"` on `<html>` (Everforest = no attribute, the default CSS
block). A stored value wins over the server default; invalid values fall back.

**Dark/light** is orthogonal and user-toggleable: dark = no `data-theme`; light =
`data-theme="light"`.

**Semantic CSS variables** (full hex in `index.css`):

| Group        | Variables                                              | Meaning                                                             |
| ------------ | ------------------------------------------------------ | ------------------------------------------------------------------ |
| Backgrounds  | `--bg-dim` → `--bg5`                                    | Page bg → raised surfaces → borders → hover (deepest to lightest)  |
| Semantic bg  | `--bg-red/-yellow/-green/-blue/-purple/-visual`        | Error, warning, success, info/active-agent, feature, selection     |
| Foreground   | `--fg`, `--grey0/1/2`                                   | Primary text, then muted → tertiary                                |
| Accents      | `--red --orange --yellow --green --aqua --blue --purple` | Priority / state / type accents (mapping below)                  |

**UI semantic mapping:**

- Card type badges: task = `--blue`, bug = `--red`, feature = `--green`,
  subtask = `--aqua`.
- Priority: critical = `--red`, high = `--orange`, medium = `--yellow`,
  low = `--grey1`.
- Card state borders: agent-active = `--aqua`, stalled = `--red`,
  unassigned = `--bg3`.
- Primary action = `--green`; secondary = `--aqua`; destructive = `--red`.
- Parent-ID badge (subtasks only): `--bg-blue` bg + `--aqua` text — same as the
  active-agent indicator.

Radix and Catppuccin map their scales onto these variables (Radix:
Slate/Tomato/Amber/Grass/Teal/Blue/Plum, accents at step 11; Catppuccin: Mocha
dark / Latte light). Hex is hardcoded in `index.css` — do not add
`@radix-ui/colors` or a Catppuccin dependency.

## Fonts

Self-hosted, no runtime CDN. `@font-face` in `src/fonts.css` (imported at the top
of `index.css`); woff2 under `public/fonts/<family>/`, emitted to `dist/fonts/`
and embedded via `web/embed.go`. Regenerate with `scripts/fontfetch.py`. System
font stacks remain as fallbacks.

## Layout & viewport constraint

**The app is pinned to viewport height at every level of the flex tree. Do not
swap a height constraint for a min-height** — that lets the page grow past the
viewport and scrolls the whole page instead of only the board columns.

Height chain (top → bottom): `#root` (`height: 100vh`) → `App` (`h-screen`) →
content area (`flex-1 flex-col min-w-0`) → `ProjectShell <main>`
(`flex-1 overflow-hidden flex-col`) → board area → `Board` (`h-full`) → columns
wrapper (`flex-1 overflow-x-auto overflow-y-hidden`) → **column card list**
(`overflow-y-auto min-h-0`, the only vertical scroller). `min-h-0` on the card
list is required so the flex child can shrink below its content height.

Mobile exception (< 768px): the Board page relaxes this so the chrome above the
kanban can scroll away — Board root adds `overflow-y-auto` (desktop keeps
`md:overflow-hidden`), the columns wrapper gets `min-h-[calc(100dvh-3rem)]`, and
`.board-footer` becomes `sticky` so the rail toggle stays reachable. No other
layer changes.

## Drag-and-drop sensors

`Board.tsx` picks a sensor by device at mount via `isTouchDevice()`
(`matchMedia('(pointer: coarse)')`, `maxTouchPoints` fallback, SSR guard):

- Touch: `TouchSensor`, `activationConstraint: { delay: 250, tolerance: 5 }` — the
  press-and-hold delay separates drag from scroll.
- Pointer: `PointerSensor`, `activationConstraint: { distance: 5 }`.

Both `useSensor()` calls run unconditionally (Rules of Hooks); the result only
selects which descriptor reaches `useSensors()`. Reuse this setup for new drag
interactions.

## Cross-cutting UI rules

- **Confirmations:** use `ConfirmModal`
  (`web/src/components/ConfirmModal/ConfirmModal.tsx`), never `window.confirm()`.
  `variant="danger"` for destructive actions. Reference integrations:
  `CardChat.tsx` (promote), `CardPanel/CardPanelDangerZone.tsx` (delete).
- **`AskUserQuestion` is denied at the MCP gate — do not build UI for it.** The
  model asks its question as an ordinary chat `text` message with options inline.
  Never add a `user_question` log type or a question card.
- **Routing catch-all:** keep `<Route path="*" element={<NotFound />} />` as the
  **last** route at both levels — `App.tsx` (top-level) and `ProjectShell.tsx`
  (nested project routes). The Go backend serves `index.html` for all non-API
  paths, so unknown-URL handling is React Router's job.
- **URL state:** for bookmarkable UI state use `useSearchParams` with the
  updater-callback form and `{ replace: true }`; validate the value against the
  known set and fall back gracefully on unknown. Current params: `?card=<id>`,
  `?project=<slug>`, `?new=1`.
- **Delivery-unit metrics:** headline counters (MetricsRibbon, KpiRow, BoardBand)
  count only cards where `parent === ""` (standalone + parent tasks); subtasks are
  excluded so decomposition doesn't inflate throughput. Backend source:
  `internal/service/service_dashboard.go`.
