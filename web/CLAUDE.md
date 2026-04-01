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
