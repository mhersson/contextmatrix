# web/ — Frontend Conventions

## Conventions

- Functional components only. No class components.
- State management: React hooks + context. No Redux.
- API calls: typed wrapper in `web/src/api/client.ts` — all endpoints in one
  file.
- CSS: Tailwind utility classes only. No CSS modules, no styled-components.
- Files: `PascalCase.tsx` for components, `useX.ts` for hooks, `types/index.ts`
  for shared types.
- Component size limit: ~150 lines. Split if larger.
- SSE: `EventSource` with exponential backoff reconnect (max 30s).
- `vite.config.ts` must proxy `/api` → `http://localhost:8080` for dev mode.
- No `localStorage` usage except: theme preference, human agent ID, last
  selected project, collapsed column/card state.
- Theme state is managed via `ThemeProvider` (in `web/src/hooks/useTheme.ts`)
  wrapping the app root. Components consume it with `useTheme()`. The markdown
  editor (`@uiw/react-md-editor`) receives `data-color-mode={theme}` so it
  tracks the active theme. Do not add a new theme mechanism — extend
  `ThemeProvider`.

## Color palette: Everforest

The web UI supports both **Everforest Medium Dark** and **Everforest Medium
Light** palettes, toggled via a sun/moon button in the header. Dark is the
default; the light variant is defined in `web/src/index.css` under
`[data-theme="light"]`. Dark mode uses `:root` with no attribute; the
`ThemeProvider` removes the `data-theme` attribute entirely for dark mode.
Define CSS custom properties in the root stylesheet and reference them
throughout all components. Do not hardcode hex values in components.

```css
:root {
  /* Backgrounds */
  --bg-dim: #232a2e; /* deepest background, page bg */
  --bg0: #2d353b; /* default background, main content area */
  --bg1: #343f44; /* raised surfaces: cards, panels */
  --bg2: #3d484d; /* popups, modals, dropdowns */
  --bg3: #475258; /* borders, dividers */
  --bg4: #4f585e; /* subtle UI elements, disabled states */
  --bg5: #56635f; /* hover states on muted elements */

  /* Semantic backgrounds */
  --bg-visual: #543a48; /* selection, drag highlight */
  --bg-red: #514045; /* error backgrounds, stalled card bg */
  --bg-yellow: #4d4c43; /* warning backgrounds */
  --bg-green: #425047; /* success backgrounds, done column hint */
  --bg-blue: #3a515d; /* info backgrounds, active agent indicator */
  --bg-purple: #4a444e; /* special/feature backgrounds */

  /* Foreground */
  --fg: #d3c6aa; /* primary text */
  --grey0: #7a8478; /* muted text, placeholders */
  --grey1: #859289; /* secondary text, timestamps */
  --grey2: #9da9a0; /* tertiary text, labels */

  /* Accent colors */
  --red: #e67e80; /* errors, critical priority, stalled state */
  --orange: #e69875; /* warnings, high priority, bugs */
  --yellow: #dbbc7f; /* caution, medium priority */
  --green: #a7c080; /* success, done state, features, primary action */
  --aqua: #83c092; /* info, active agent, links */
  --blue: #7fbbb3; /* secondary info, tasks */
  --purple: #d699b6; /* special, labels, metadata */
}
```

**Mapping to UI semantics:**

- Card type badges: task=`--blue`, bug=`--red`, feature=`--green`,
  subtask=`--aqua`
- Priority indicators: critical=`--red`, high=`--orange`, medium=`--yellow`,
  low=`--grey1`
- Card state borders: agent-active=`--aqua`, stalled=`--red`, unassigned=`--bg3`
- Column headers: use `--grey2` text on `--bg0`
- Interactive elements (buttons, links): `--green` primary, `--aqua` secondary
- Destructive actions: `--red`
- Parent ID badge: `--bg-blue` background, `--aqua` text — same palette as the
  active-agent indicator. Only rendered on subtask cards (`card.parent` defined).

## Layout and viewport constraints

The app is constrained to exactly the browser viewport height at every level of
the flex tree. **Do not change height constraints to minimum-height constraints**
— doing so allows the page to grow beyond the viewport and causes the entire
page to scroll instead of only the board columns.

### Height chain (top → bottom)

| Layer | Element / File | Class / Rule |
|---|---|---|
| Root | `#root` in `web/src/index.css` | `height: 100vh` |
| App | outer `div` in `App.tsx` | `h-screen flex flex-row` |
| Content area | right-side `div` in `App.tsx` | `flex-1 flex flex-col min-w-0` |
| ProjectShell | `<main>` in `ProjectShell.tsx` | `flex-1 overflow-hidden` |
| Board | root `div` in `Board.tsx` | `flex flex-col h-full` |
| Columns wrapper | inner `div` in `Board.tsx` | `flex-1 overflow-x-auto overflow-y-hidden` |
| Column card list | scroll container in `Column.tsx` | `overflow-y-auto min-h-0` |

The only element that scrolls vertically is the column card list. Everything
above it in the tree has a fixed height. `min-h-0` on the card list is required
because flex children default to `min-height: auto`, which would prevent them
from shrinking below their content height and break the overflow.

The Sidebar uses `flex flex-col h-full`, keeping the "New Project" button pinned
in its footer (`border-t` section) at the bottom of the viewport at all times,
regardless of how many projects are listed.

### Horizontal scrolling

Columns scroll horizontally inside the columns wrapper (`overflow-x-auto`), with
`overflow-y-hidden` preventing any vertical escape at that level.

## Subtask parent navigation

Subtask cards display their parent card ID as a clickable badge. Clicking it
navigates to the parent card (same handler as subtask navigation).

**CardItem (board view):**

- Collapsed view: badge appears in the header row between the type badge and
  card title.
- Expanded view: badge appears in the footer row alongside the priority dot,
  agent indicator, and labels.
- The badge uses `e.stopPropagation()` so clicking it does not open the subtask
  card itself.
- Prop: `onParentClick?: (cardId: string) => void` — threaded through
  `ProjectShell → Board → Column → CardItem`.
- `ProjectShell` wires `onParentClick={handleSubtaskClick}`, reusing the same
  handler that navigates to subtasks (card-by-ID lookup in local state).

**CardPanelMetadata (detail panel):**

- A "Parent" section is rendered above "Subtasks" when `card.parent` is defined.
- The parent ID button reuses the existing `onSubtaskClick` prop — no new prop
  required.
- Styling: `--bg-blue` background, `--aqua` text, monospace font — consistent
  with subtask ID buttons.

**Known UX notes (tracked for future polish):**

- In the expanded CardItem footer, the parent badge and agent indicator share
  identical colours (`--bg-blue`/`--aqua`). They are visually distinguishable
  only by content (card ID vs. agent name). `title` tooltips disambiguate on
  hover. A future pass may add a small icon prefix or border to the parent badge.
- The parent badge button in CardItem has no `aria-label`. Adding
  `aria-label={\`Navigate to parent \${card.parent}\`}` would improve screen
  reader accessibility.
