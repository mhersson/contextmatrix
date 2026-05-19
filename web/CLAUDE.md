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
- SSE: one shared `EventSource('/api/events')` owned by `SSEProvider`
  (`web/src/hooks/useSSEBus.tsx`). Consumers call `useSSEBus()` and register
  handlers via `subscribe(onEvent): () => void` inside a `useEffect`; the return
  value is the unsubscribe cleanup. Exponential-backoff reconnect (1s → 30s max)
  lives in the provider. Do not open additional `EventSource('/api/events')`
  connections — Firefox cancels in-flight SSE requests to the same origin with
  `NS_BINDING_ABORTED` when too many connections hit concurrently (see
  `docs/gotchas.md`).
- `vite.config.ts` must proxy `/api` → `http://localhost:8080` for dev mode.
- No `localStorage` usage except: theme preference, palette preference
  (`palette` key), human agent ID, last selected project, last chat id
  (`last_chat_id`), chat section collapse (`sidebar.chat_section_collapsed`),
  multi-pane chat layout (`chat_layout`), collapsed column/card state, chat
  filter preferences (`chat_filter_prefs`).
- Theme state is managed via `ThemeProvider` (in `web/src/hooks/useTheme.ts`)
  wrapping the app root. Components consume it with `useTheme()`. The markdown
  editor (`@uiw/react-md-editor`) receives `data-color-mode={theme}` so it
  tracks the active theme. Do not add a new theme mechanism — extend
  `ThemeProvider`.

## Color palettes

The web UI supports three color palettes: **Everforest** (default),
**Radix**, and **Catppuccin**.

### Palette selection

The server config (`theme` in `config.yaml`, env: `CONTEXTMATRIX_THEME`) sets
the **default** palette. On startup `ThemeProvider` fetches
`GET /api/app/config` and applies `data-palette="<theme>"` on `<html>` for
every palette except Everforest, which is the default CSS block (no attribute).

Users can override the palette via the **PaletteSelector** dropdown in
`AppHeader` (next to the dark/light toggle). Selecting a palette calls
`setPalette()` from `useTheme`, which updates `data-palette` and writes the
choice to `localStorage` under the key `palette`. On subsequent page loads,
`ThemeProvider` reads this stored value first; if present and valid it applies
immediately and skips the server default. The stored value must be one of
`"everforest"`, `"radix"`, `"catppuccin"` — invalid values are ignored and
fall back to the server default.

Dark/light mode is **user-toggleable** (sun/moon button) and orthogonal to
palette. Dark mode: no `data-theme` attribute. Light mode: `data-theme="light"`.

Both palettes define the same CSS custom properties. Components need no changes
when the palette is switched — all styling references CSS variables only. Do not
hardcode hex values in components.

### Everforest palette

Defined in `:root` (dark, default) and `[data-theme="light"]` (light) in
`web/src/index.css`. Dark mode uses the **Medium** variant (`:root`, no
`data-theme` attribute); `ThemeProvider` removes the attribute entirely for
dark mode. Light mode uses the **Hard** variant (`[data-theme="light"]`) for
higher text-on-background contrast.

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

### Radix palette

Activated when `data-palette="radix"` is present on `<html>`. Defined in two
blocks in `web/src/index.css`: `[data-palette="radix"]` (dark) and
`[data-palette="radix"][data-theme="light"]` (light).

Hue assignments:

| CSS variable group | Radix scale |
|---|---|
| Gray (`--bg-*`, `--grey*`) | Slate |
| `--red` / `--bg-red` | Tomato |
| `--orange` | Orange |
| `--yellow` / `--bg-yellow` | Amber |
| `--green` / `--bg-green` | Grass |
| `--aqua` / `--bg-blue` | Teal |
| `--blue` | Blue |
| `--purple` / `--bg-purple` | Plum |
| `--bg-visual` | Plum |

Step-to-role mapping (applies to every hue):

| Steps | Role |
|---|---|
| 1–2 | `--bg-dim`, `--bg0` — page/main backgrounds |
| 3–5 | `--bg1`, `--bg2`, semantic backgrounds (`--bg-red` etc.) |
| 6–8 | `--bg3`, `--bg4`, `--bg5` — borders, disabled, hover |
| 10–11 | `--grey0`, `--grey1`, `--grey2` — muted/secondary text |
| 12 | `--fg` — primary foreground text |
| 11 (accent) | `--red`, `--orange`, `--yellow`, `--green`, `--aqua`, `--blue`, `--purple` |

Accents use Radix **step 11** (the "low-contrast text" step) rather than step
10, because accents are used primarily as small text on background steps 1–2
where step 10 reads as too dim.

Hex values are hardcoded in `index.css`. Do not add `@radix-ui/colors` as an
npm dependency.

### Catppuccin palette

Activated when `data-palette="catppuccin"` is present on `<html>`. Defined in
two blocks: `[data-palette="catppuccin"]` uses the **Mocha** flavor (dark) and
`[data-palette="catppuccin"][data-theme="light"]` uses the **Latte** flavor
(light). Background hierarchy follows the Catppuccin Crust/Mantle/Base/Surface
scale; accent assignments are: `--red` = Red, `--orange` = Peach, `--yellow` =
Yellow, `--green` = Green, `--aqua` = Teal, `--blue` = Blue, `--purple` =
Mauve.

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

## CardPanel active-session layout

The split layout and session chat are **HITL-only**. Both are gated on a
derived boolean:

```ts
const isHITLRunning = card.runner_status === 'running' && !card.autonomous;
```

When `isHITLRunning` is true, `CardPanel` switches from its normal
single-scroll body to a **split layout** that gives the Session Chat maximum
vertical space. When false (autonomous run, idle, or any other state),
the single-body layout is used and `CardChat` renders nothing.

### Split-body structure (HITL runs only)

```
<div data-testid="body-split">          flex flex-col flex-1 min-h-0
  <div data-testid="body-top-section">  overflow-y-auto max-h-[50%] — Agent, Description, Metadata, Activity
  <div data-testid="body-chat-region">  flex-1 min-h-0 — CardChat fills remaining height
```

Autonomous runs (`card.autonomous === true`) always use the single-scroll
wrapper (`data-testid="body-single"`) even while `runner_status === 'running'`,
because their chat region would otherwise be empty.

`CardChat` returns null (renders nothing) when
`card.runner_status !== 'running' || card.autonomous`. This hides the log
panel, the textarea, the Send button, and the "Switch to Autonomous" button
together. When a HITL→Auto promotion occurs mid-run, `card.autonomous` flips
to `true`, the component re-renders, and the entire chat UI disappears
immediately.

`CardChat` root is `flex flex-col h-full`; its log container is
`flex-1 min-h-[60px]` (not `max-h-[200px]`), so it expands to fill the chat
region. The input row and action buttons stay pinned at their natural height
below the log.

### Message type filter bar

A thin filter row is rendered at the top of the chat panel, above the message
list. It contains three labeled checkboxes that control which message types are
visible:

| Checkbox | Default | Controls | Label color |
|---|---|---|---|
| Text | on | `type === 'text'` | `--fg` |
| Tool calls | off | `type === 'tool_call'` | `--aqua` |
| Thinking | off | `type === 'thinking'` | `--grey2` |

Types `user`, `stderr`, `system`, and `gap` are always shown regardless of the
filter state — they carry user messages and diagnostic information that must
not be hidden.

Filtering is applied at render time against the full in-memory buffer
(`cardLogs`). Toggling a filter on retroactively reveals all messages of that
type received since the session started. Filter state is persisted to
`localStorage` under the key `chat_filter_prefs` (JSON `{ showText,
showToolCalls, showThinking }`) and restored when the panel re-mounts or
the page reloads. Defaults (`showText: true`, `showToolCalls: false`,
`showThinking: false`) apply when the key is missing, malformed, or any field
is not a boolean.

State lives in the `useChatFilterPrefs` hook
(`web/src/hooks/useChatFilterPrefs.ts`), which owns the load/save round-trip
and exposes `{ prefs, setPref }`. `ChatPanel.tsx` consumes the hook; no new
props or context is involved.

### Rail tabs + default tab on HITL

The bifold drawer no longer uses section-level collapsibles for Description,
Labels, and Automation. Instead, the right-side rail exposes **tabs**
(`Automation`, `Info`, `Danger`, plus `Chat` when an HITL session is running)
and only the active tab's content is mounted. This replaces the previous
per-section chevron pattern; there is no `descriptionCollapsed` /
`labelsCollapsed` / `automationCollapsed` state anymore.

**Default tab** is derived from `isHITLRunning`:

```ts
const defaultTab: RailTabKey = isHITLRunning ? 'chat' : 'automation';
```

**Rail auto-expand** mirrors the default-tab logic: `railExpanded` initial state
is `useState(isHITLRunning)`, so opening a card with an active HITL run starts
the rail expanded. The user can still manually collapse it after auto-expand.

On transitions of the sync inputs, UI state is reset as follows:

- **Card identity change** (`sync.cardId !== card.id`): full reset —
  `editedCard`, `railExpanded → isHITLRunning`, forced-flag badges, and
  `activeTab → defaultTab`. Switching to a HITL card expands the rail;
  switching to a non-HITL card collapses it.
- **Same card, new object reference from SSE** (`sync.card !== card`, same
  id): `editedCard` refreshes so unedited fields reflect server-side
  updates. `railExpanded`, forced flags, and `activeTab` are preserved so
  agent-driven state transitions and log updates do not disrupt a user
  mid-HITL-session.
- **`isHITLRunning` flip to `true`** (e.g. clicking "Run HITL" while the
  panel is open): resets `activeTab → defaultTab` and sets
  `railExpanded → true`.
- **`isHITLRunning` flip to `false`** (e.g. HITL→Auto promotion mid-run,
  or a run ending): collapses the chat tab back to `defaultTab`, but only
  after **two consecutive sync events** have observed `isHITLRunning ===
  false`. The counter (`sync.hitlOffCount`) increments on each sync that
  still sees the run as off and triggers the tab reset on reaching 2. A
  single transient SSE glitch (e.g. `runner_status` momentarily stale)
  therefore does not switch the tab away from `chat`. The counter resets
  on a HITL-on flip, on card-id change, and on any user-initiated tab
  change. `railExpanded` is preserved throughout.

The transitions are handled in a single in-render `useState` marker block
(`sync`, keyed by `cardId`, carrying `card`, `isHITLRunning`, and
`hitlOffCount`) in `CardPanel.tsx` — not a `useEffect` — so the reset is
synchronous with the prop change and avoids the double-render that a
reactive effect would cause. The debounce counter lives in this same
state object (not a `useRef`) to comply with the `react-hooks/refs` lint
rule, which forbids reading or writing refs during render.

Mounting into an already-HITL card lands on the `chat` tab and starts with the
rail expanded via the initial `useState(isHITLRunning)` — no transition needed.

## Global Chat tab

The Chat tab (`/chat`, `/chat/:id`) hosts long-lived chat sessions distinct
from card-scoped HITL chats. `useChatStream(sessionID)` owns transcript
state for the active session via `useRingBuffer(2000)`. The hook pairs the
SSE `/api/chats/{id}/stream` subscription with a REST bootstrap from
`GET /api/chats/{id}/messages?since_seq=0&limit=1000`:

1. On mount or `sessionID` change, the buffer is cleared and the REST
   bootstrap is fetched first.
2. The last bootstrap `seq` is recorded; the SSE subscription opens with
   `since_seq=<last>` so it only delivers strictly newer events.
3. Replay overlap (SSE events whose `seq` falls inside the REST window) is
   deduped on the client — the seam is gapless without double messages.

The hook also listens on the `session_updated` named SSE channel and merges the
payload into `sessionUpdate: ChatSessionUpdate | null` state. When the incoming
payload contains a `status` field that differs from the previous value, the hook
calls `notifyChatSessionsChanged()` to trigger a sidebar refetch — this is how
lifecycle transitions (`warm-idle → active`, `active → warm-idle`) propagate to
the sidebar status dot without a full page reload.

`ChatSessionUpdate` (`web/src/types/index.ts`):

```typescript
export interface ChatSessionUpdate {
  context_tokens?: number;
  context_tokens_updated_at?: string;
  model?: string;
  rehydration_active?: boolean;
  status?: ChatStatus;  // present only when the lifecycle state changed
}
```

`last_chat_id` localStorage key tracks the focused pane's chat. In the
multi-pane layout (see next section), `useChatLayout` writes the key
whenever focus moves; `ChatThread` only writes it in non-embedded (mobile
single-pane) mode. This preserves backward compat with external readers
that expect a single "current chat" pointer.

## Multi-pane chat layout

`/chat` renders a tiled layout of up to 4 simultaneously open chats. The
shell is `ChatLayout` (`web/src/components/ChatLayout/`), composed with
`react-resizable-panels` `PanelGroup`s for the column + row splits. State
lives in `useChatLayout` (`web/src/hooks/useChatLayout.ts`) and is threaded to descendants via props (`ChatLayoutContext` is exported for future composition but currently has no consumers).

### Layout model

Panes are addressed by `Slot` ('TL' | 'BL' | 'TR' | 'BR'). The hook
normalizes the layout so:

- 1 pane → only `TL` is occupied (full-width).
- 2 panes → `TL` + `TR` (vertical split).
- 3 panes → either left or right column has a horizontal split (whichever
  column held the focused pane when the 3rd pane was opened).
- 4 panes → 2×2 grid.

Closing a pane runs `normalize()` to collapse the layout (e.g. closing
`TL` promotes `BL → TL`). Column- and row-percentages persist as
`{ col, leftRow, rightRow }` and are clamped 20–80% by the resizable-
panels library.

### Mutations

- `openInNewPane(id)` / `openInFocused(id)` — same implementation in v1:
  sidebar clicks always auto-tile into a new pane (per the captured build
  prompt — no `Cmd`-click distinction). If the chat is already open in
  another pane, focus that pane instead of opening a duplicate.
- `swapPaneChat(slot, id)` — drop semantics. If the dropped chat is
  already in another pane, the two panes' contents **swap** (same-chat-
  twice = swap). If not, the target pane's contents are replaced.
- `splitFromPane(slot)` / `cancelEmptyPane(slot)` — manual "+ split"
  button creates an empty pane with a "Pick a chat" picker; Esc cancels.
- `closePane(slot)` — removes the pane (the chat session itself is **not**
  deleted; the End / Reopen / Delete actions live on `ChatThread`'s
  non-embedded header and are reachable on mobile or by closing other
  panes down to one).
- `movePane(fromSlot, toSlot)` — unconditional swap of the two slots' contents (including null-on-either-side); then `normalize()` collapses any empty slots. Focus follows the dragged chat: `focused = toSlot` when the source had a chat; `lastFocusedAt[toSlot]` is stamped. No-op when `fromSlot === toSlot`. Used exclusively by pane-header drags (see below).
- `focus(slot)` — stamps `lastFocusedAt[slot] = Date.now()` for LRU.

### 5th-chat policy: LRU eviction with undo

When `openInNewPane` is called and 4 panes are open, the hook evicts the
pane with the smallest `lastFocusedAt` stamp, calls `onLRUEvict({
victimSlot, victimChatId, incomingChatId, snapshot })`, and `ChatPage`
shows a `chat-evict-toast` (bottom-center, 6s) with an Undo button. Undo
calls `restoreSnapshot(snapshot)` to atomically revert.

### Persistence

- `localStorage.chat_layout`: `{ panes, focused, sizes, lastFocusedAt }`,
  debounced 300ms. On mount, `loadPersisted()` filters persisted chat IDs
  against the current `availableChats` list (dropping stale ids).
- `localStorage.last_chat_id`: written by `useChatLayout` whenever focus
  moves (focused pane's chat id only).
- Server-side deletes are reconciled via an effect that watches
  `availableChats`: ids no longer in the list are removed from panes.

### Drag-and-drop

Two drag sources feed the same drop targets (panes):

**Sidebar drags** (`ChatSection`): `ChatSection` lives outside
`ChatLayoutProvider`'s subtree (the sidebar renders above the route
outlet). It sets `draggable=true` on chat rows and writes only
`text/plain` (chat id) to `dataTransfer`. To let pane drop-overlays show
the incoming chat name, `ChatSection` dispatches `cm:chat-drag-start` /
`cm:chat-drag-end` custom events; `ChatPage` listens and forwards to
`layout.setDragging(...)`. Drop routes to `onDropChatOnPane` →
`swapPaneChat`. Touch devices skip `draggable=true` (`!isTouchDevice()`
guard).

**Pane-header drags** (`PaneHeader`): the entire `chat-pane-header` div is
a drag source when `!isTouchDevice() && chatId != null` (the parent
`ChatPane` computes this and passes `draggable={headerDraggable}` to
`PaneHeader`). The header gets `cursor: grab` via inline style when
draggable. `onDragStart` writes two MIME types: `text/plain` (chat id) and
the custom `application/x-cm-pane-source-slot` (the source `Slot` string,
e.g. `"TL"`). It also dispatches the same `cm:chat-drag-start` window event
so the drop overlays activate. Action buttons (`chat-pane-btn`) carry
`draggable={false}` + `onDragStart={(e) => e.preventDefault()}` to prevent
accidental header drags originating from a button area.

**Drop routing in `ChatPane.handleDrop`**: reads
`application/x-cm-pane-source-slot` first. If present and the source slot
differs from the target, calls `onMovePane(fromSlot)` →
`layout.movePane(fromSlot, toSlot)` (unconditional swap). If the source
MIME is absent, falls through to the sidebar path: reads `text/plain` →
`onDropChat` → `swapPaneChat`. Same-pane drops (source MIME == target slot)
are a no-op. An unrecognised slot value (not in `SLOTS`) is treated as a
malformed drop and falls through to the sidebar path for forward-compat.

**Protocol constants** (`PANE_SOURCE_MIME`, `CHAT_DRAG_START_EVENT`,
`CHAT_DRAG_END_EVENT`) are the single source of truth defined in
`web/src/components/ChatLayout/dragProtocol.ts`. Import from there; do not
re-declare them locally.

### Routing

- `/chat` — hydrates the layout from `chat_layout`, renders `ChatLayout`.
- `/chat/:id` — **additive** deep link. The id is opened as a new pane on
  top of the hydrated layout (LRU evicts the 5th), then `ChatPage`
  redirects to `/chat` so refresh doesn't re-trigger. Uses the in-render
  state-marker pattern (`prevDeepLinkId !== deepLinkId`) — not `useEffect`
  — so the navigate happens synchronously with the prop change.
- `/chat?new=1` — opens `NewChatDialog`.

### Mobile (`< 768px`)

`useMediaQuery('(min-width: 768px)')` toggles single-pane mode. The hook's
state persists across resizes; only one pane (the focused one) is
rendered. Sidebar drag is disabled on touch devices. The full
`ChatThread` (with its End / Reopen / Delete header) is rendered
*non-embedded* on mobile so all chat actions stay reachable.

### Visual tokens

All chat-pane CSS lives at the end of `web/src/index.css` under the
"Multi-pane chat layout" header. Mirrors CardPanel: 36px header, mono
font, `.bf-rail-tab` typography, `--bg0` body, `--bg1` header bg,
`--bg3` borders, `--aqua` focused-state glow + resize-handle hover.
Per-chat 2px accent stripe colored by `idColor(chatId)` from
`web/src/utils/colorHash.ts` (shared with RunnerConsole). Drop target
uses the **static glow** variant (no pulse animation).

## Runner Console

The Runner Console is a live log panel that streams output from
`contextmatrix-runner` containers while they execute autonomous tasks.

### Visibility and connection lifecycle

The console is gated on `remote_execution.enabled` for the current project.
The `EventSource` is opened only while the panel is visible — no background
streaming. `useRunnerLogs` connects on mount when `enabled=true` and
disconnects on `enabled=false` or component unmount.

### AppHeader integration

When `runnerEnabled` is true, a **Console** button (`>_` icon) is rendered
inside the VIEWS pill group between **Board** and **Knowledge**. It behaves
like a toggle, not a NavLink — it calls `onToggleConsole` rather than
navigating. Props added to `AppHeaderProps`:

| Prop | Type | Purpose |
|---|---|---|
| `runnerEnabled` | `boolean?` | Controls whether the Console button is shown |
| `consoleOpen` | `boolean?` | Active highlight on the button |
| `onToggleConsole` | `() => void?` | Toggle handler |

Keyboard shortcut: `c` (registered in `useKeyboardShortcuts`; only fires when
no panel is open and `remote_execution.enabled` is true).

### Create & Run auto-open

When a user submits a new card via the **Create & Run HITL** or **Create & Run Auto** button, `ProjectShell` automatically opens the `CardPanel` for the newly created card. The create form closes as usual, and the user is taken directly to the card detail view.

This is handled in the `onCreateCard` callback (`ProjectShell.tsx`): when `opts?.run` is true, `api.runCard` is called after card creation, and on success `setSelectedCard(updatedCard)` opens the panel with the card's updated `runner_status` / `assigned_agent`. The board flash (`setFlashCardId`) is skipped for the run path — it fires only for plain "Just create" submissions.

| Action | Result |
|---|---|
| Just create | Create form closes, board flashes the new card. No panel opens. |
| Create & Run HITL | Create form closes, `CardPanel` opens. User sees chat immediately. |
| Create & Run Auto | Create form closes, `CardPanel` opens. User sees runner status immediately. |

### ProjectShell layout

`ProjectShell` owns the console state and the log data. Its `<main>` is a
`flex-col` container. When the console is open the board area and console
use percentage-based flex basis controlled by `useResizeDivider`. Default
split is 60/40 (board/console). A draggable divider between them lets the
user resize. The transition is `transition-all duration-300` (disabled
during active drag to avoid lag).

```
<main ref={mainRef} className="flex-1 overflow-hidden flex flex-col">
  <div style={{ flex: consoleOpen ? `0 1 ${boardPercent}%` : '1 1 100%' }}>
    {/* Board / Settings / Knowledge routes */}
  </div>
  {consoleOpen && (
    <>
      <div {...dividerHandleProps}>{/* resize pill */}</div>
      <RunnerConsole flexBasis={`${100 - boardPercent}%`} ... />
    </>
  )}
</main>
```

### Resizable divider

`useResizeDivider` (`web/src/hooks/useResizeDivider.ts`) uses native Pointer
Events with `setPointerCapture` for cross-device (mouse + touch) drag. Returns
`{ boardPercent, isDragging, handleProps }`. Constraints: board min 20%,
console min 15%. During drag, `document.body.style.userSelect` is set to
`'none'` and cursor to `'row-resize'` (restored on pointer up or unmount).
The divider element sets `touch-action: none` to prevent browser gesture
interference on touch devices.

### Component tree

| File | Role |
|---|---|
| `web/src/hooks/useRingBuffer.ts` | Fixed-capacity circular buffer hook. `useRingBuffer(maxEntries)` returns `{ logs: readonly LogEntry[], append(entries), clear() }`. Backed by `createRingBufferStore` — a plain JS store exposing `subscribe`, `getSnapshot`, `append`, `clear`. Maintains a pre-allocated array and `head` index; `append` writes at `head` and advances modulo capacity (O(1) per entry, no array copy). The snapshot array is built lazily on `getSnapshot` and cached against a monotonic version counter, so it is rebuilt at most once per render regardless of how many appends fired between commits. `useRingBuffer` reads via `useSyncExternalStore` for concurrent-safe reads across StrictMode/Suspense/transitions. Capacity is clamped to ≥ 1. |
| `web/src/hooks/useRunnerLogs.ts` | EventSource hook. `{ project, enabled, maxEntries=5000, cardId? }`. When `cardId` is set, connects to the card-scoped session endpoint (`?project=P&card_id=X`). Without `cardId`, connects to the project-scoped session endpoint (`?project=P`). Both paths replay the server-side snapshot on connect so no events are lost across reconnects. Delegates all log-array management to `useRingBuffer`. Clears the buffer when opening the stream (`enabled` becoming `true`), or when `project`/`cardId` changes, so a fresh server-snapshot replay on reconnect does not duplicate entries. Exponential backoff reconnect (1s → 30s max). Tracks last-seen `seq` and inserts a `gap` marker on discontinuity. `dropped` frames render as gap markers. `terminal` frames stop the reconnect loop and clear `connected` — but only if at least one log entry has been delivered during the current connect cycle (`logsReceivedRef > 0`). A `terminal` arriving on an empty buffer is treated as the session-manager fast-path race (server emitted terminal before any snapshot frames) and triggers a backoff reconnect instead of halting, so the next connect can pick up a clean snapshot. `usage` frames (token-accounting metadata) are silently dropped before the ring buffer — they carry no display value and are consumed via the separate `session_updated` SSE path. `lastSeqRef.current` is still advanced for `usage` frames before the early return, because `seq` is a unified monotonic counter across all entry types; skipping it would cause a phantom gap marker on the next renderable frame. Returns `{ logs, connected, error, clear }`. |
| `web/src/hooks/useResizeDivider.ts` | Pointer-event-based resize hook. Returns `{ boardPercent, isDragging, handleProps }`. Spread `handleProps` onto the divider element. |
| `web/src/components/RunnerConsole/RunnerConsole.tsx` | Root component. Owns `cardFilter` state. Derives `uniqueCardIds` and `filteredLogs` via `useMemo`. |
| `web/src/components/RunnerConsole/RunnerConsoleHeader.tsx` | Header bar: title, connection dot (green/red), card-ID filter `<select>`, Clear button, Close button. |
| `web/src/components/RunnerConsole/RunnerConsoleLog.tsx` | Thin wrapper that passes `logs` into `VirtualLogList` with the correct ARIA attributes (`role="log"`, `aria-live="polite"`). |
| `web/src/components/RunnerConsole/VirtualLogList.tsx` | Variable-height virtualised list. Measures each rendered row via `ResizeObserver` and caches heights in an external `HeightStore`. Cumulative offsets are recomputed via `useMemo` whenever `items.length`, `heightStore`, or `heightVersion` changes — `heightVersion` is the value returned by `useSyncExternalStore(heightStore.subscribe, heightStore.getSnapshot)` and ensures the offset array stays in sync with measured heights (not estimate-only). Binary search on the offset array picks the visible window. Auto-scrolls to the true content-bottom on new items unless the user has scrolled up (threshold: 50 px from bottom). Reopening the console always lands at the end of the log; no scroll-position restore across mounts. |

### LogEntry type (`types/index.ts`)

```typescript
export type LogEntryType = 'text' | 'thinking' | 'tool_call' | 'stderr' | 'system' | 'user' | 'gap';

export interface LogEntry {
  ts: string;        // ISO timestamp (matches Go json:"ts" tag)
  card_id: string;
  type: LogEntryType;
  content: string;
  seq?: number;      // monotonic sequence number from the server
}
```

`'gap'` is a client-side-only synthetic type inserted by `useRunnerLogs` when:
- a `dropped` server frame is received (ring-buffer overflow), or
- a sequence discontinuity is detected (`seq > lastSeq + 1`).

Gap entries are never sent by the server; they exist only in the frontend log
array to surface delivery holes visibly.

The `project` field sent by the runner is not included in the frontend
`LogEntry` interface (it is available in the SSE payload but unused in the UI).

### Log line colours

| type | CSS variable |
|---|---|
| `thinking` | `--grey2` |
| `text` | `--fg` |
| `tool_call` | `--aqua` |
| `stderr` | `--yellow` |
| `system` | `--green` |
| `user` | `--blue` |
| `gap` | `--orange` |

Timestamps use `--grey1`. Card ID badges use a deterministic colour hash over
`--blue`, `--purple`, `--aqua`, `--orange`, `--yellow`.

## Layout and viewport constraints

The app is constrained to exactly the browser viewport height at every level of
the flex tree. **Do not change height constraints to minimum-height constraints**
— doing so allows the page to grow beyond the viewport and causes the entire
page to scroll instead of only the board columns.

This rule applies to the desktop layout (≥ `md`). On phone-sized viewports
(< `768px`) the Board page intentionally relaxes the constraint so the chrome
above the kanban (BoardBand · MetricsRibbon · SpotlightStrip · FilterChipBar)
can scroll out of the way, letting the columns claim the viewport. The
relaxation is local to the Board page: the Board root adds `overflow-y-auto`
on mobile (kept as `md:overflow-hidden` on desktop), the columns wrapper
gets `min-h-[calc(100dvh-3rem)]` on mobile (kept as `flex-1 min-h-0` on
desktop), and `.board-footer` becomes `position: sticky; bottom: 0` inside
the `@media (max-width: 768px)` block so the rail-toggle stays reachable
throughout the scroll. No other height in the chain (root, App, ProjectShell)
is touched — they all remain pinned to viewport height.

### Height chain (top → bottom)

| Layer | Element / File | Class / Rule |
|---|---|---|
| Root | `#root` in `web/src/index.css` | `height: 100vh` |
| App | outer `div` in `App.tsx` | `h-screen flex flex-row` |
| Content area | right-side `div` in `App.tsx` | `flex-1 flex flex-col min-w-0` |
| ProjectShell | `<main>` in `ProjectShell.tsx` | `flex-1 overflow-hidden flex flex-col` |
| ProjectShell board area | inner `div` in `ProjectShell.tsx` | `overflow-hidden transition-all duration-300` (flex grows to fill remaining height; shrinks to ~50% when console is open) |
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

## Mobile touch and drag-and-drop

Drag-and-drop uses different sensors for touch and pointer (mouse) devices.
`Board.tsx` calls `isTouchDevice()` at mount time to select the sensor:

- **Touch devices:** `TouchSensor` with `activationConstraint: { delay: 250, tolerance: 5 }`.
  The 250ms press-and-hold delay distinguishes intentional drag from scroll gestures.
- **Pointer devices:** `PointerSensor` with `activationConstraint: { distance: 5 }`.
  A 5px movement threshold before drag activates.

Both `useSensor()` calls are always executed unconditionally (React Rules of
Hooks). The `isTouchDevice()` result selects which descriptor to pass to
`useSensors()`.

`isTouchDevice()` uses `window.matchMedia('(pointer: coarse)')` with a
`navigator.maxTouchPoints > 0` fallback and an SSR guard
(`typeof window === 'undefined'`). The result is treated as stable for the page
lifetime.

**When adding new drag interactions:** use the existing sensor setup. Do not
create separate sensor configurations without updating this documentation.

## Mobile sidebar

On viewports narrower than `768px` (Tailwind `md` breakpoint) the desktop
sidebar is hidden by the `.sidebar` CSS rule in `web/src/index.css`. A mobile
drawer replaces it.

### Architecture

| File | Role |
|---|---|
| `web/src/context/MobileSidebarContext.tsx` | `MobileSidebarProvider` + `useMobileSidebar` hook. Owns the `isOpen` boolean. Exports `toggle` and `close` (both stable via `useCallback`). |
| `web/src/App.tsx` | Wraps `AppInner` with `MobileSidebarProvider` (inside `ThemeProvider`). `AppInner` reads `isOpen`/`close` and passes them as `mobileOpen`/`onMobileClose` to `Sidebar`. |
| `web/src/components/Sidebar/Sidebar.tsx` | Accepts `mobileOpen?: boolean` and `onMobileClose?: () => void`. When `mobileOpen` is true, renders a fixed overlay (backdrop + drawer panel) instead of the normal desktop sidebar. |
| `web/src/components/AppHeader/AppHeader.tsx` | Consumes `useMobileSidebar` and renders a hamburger button (`md:hidden`) that calls `toggle()`. |

### Rendering modes

`Sidebar` has three mutually exclusive render paths:

1. **Mobile overlay** — `mobileOpen === true`: renders `fixed inset-0 z-50`
   dark backdrop + `fixed left-0 top-0 h-full z-50` drawer panel. Both share
   the `panelContent` fragment with the desktop view.
2. **Desktop collapsed** — `mobileOpen === false && collapsed === true`: 48 px
   icon-only strip with an expand button.
3. **Desktop expanded** — default: 240 px panel with `.sidebar` class (hidden
   by CSS on mobile).

### Closing the drawer

The drawer closes on any of these events (all call `onMobileClose`):
- Tap the dark backdrop (`onClick` on the backdrop `div`)
- Tap the X button in the drawer header
- Navigate to any project link or "All Projects"

Note: the "New Project" button does **not** call `onMobileClose` — the wizard
modal covers the screen anyway, but this can be improved in a future pass
(`onClick={() => { onNewProject(); onMobileClose?.(); }}`).

### Known limitations

- The hamburger button is rendered inside `AppHeader`, which is only present
  within `ProjectShell`. The `/all` route has no hamburger. Low impact because
  the All Projects dashboard itself lists all projects. The `/chat` route
  renders its own `MobileChatHeader` (`web/src/pages/Chat/MobileChatHeader.tsx`)
  with a hamburger that reuses `useMobileSidebar`, plus the focused chat title
  and a "+ new chat" button.
- The desktop collapsed sidebar (48 px strip, no `.sidebar` class) does not
  hide on mobile — pre-existing issue unrelated to this feature.
- `MobileSidebarContext.tsx` exports both a component and a hook from the same
  file, which triggers the `react-refresh/only-export-components` lint warning.
  This is consistent with the pre-existing `useProjects.tsx` pattern.

## Mobile Knowledge Base

On viewports narrower than `768px` (Tailwind `md` breakpoint) the Knowledge
Base tab replaces the always-visible sidebar with a slide-in sheet triggered by
the doc-title row. Desktop layout is unchanged.

### Architecture

| File | Role |
|---|---|
| `web/src/components/KnowledgeBase/MobileDocSheet.tsx` | Backdrop overlay + right-side slide-in panel (reuses `card-panel` and `animate-panel-slide-in` CSS classes). Renders `KnowledgeBaseSidebar` inside. Accepts all sidebar props plus `onClose: () => void`. Calls `onClose` after a doc is selected (intercepts `onSelect`). |
| `web/src/components/KnowledgeBase/MobileDocTrigger.tsx` | Shared `md:hidden` trigger row: open-book icon + doc name (or "Choose a document" in `--grey1` when undefined) + chevron-right. Props: `docName?: string; onClick: () => void`. Used by `KnowledgeBase.tsx` fallback branches and by `KnowledgeDocViewer`/`KnowledgeDocEditor`. |
| `web/src/components/KnowledgeBase/KnowledgeBase.tsx` | Owns `isSheetOpen: boolean` state. Applies `hidden md:flex` to the sidebar wrapper. Renders `<MobileDocSheet>` when `isSheetOpen` is true. Passes `onOpenSelector={() => setIsSheetOpen(true)}` to `KnowledgeDocViewer`. Also renders `<MobileDocTrigger>` in the "No KB docs yet" and "Select a doc." fallback branches so mobile users always have a way to open the sheet. |
| `web/src/components/KnowledgeBase/KnowledgeDocViewer.tsx` | Accepts `onOpenSelector?: () => void`. Renders `<MobileDocTrigger docName={doc}>` at the top when `onOpenSelector` is set. Forwards `onOpenSelector` to `KnowledgeDocEditor` when editing. |
| `web/src/components/KnowledgeBase/KnowledgeDocEditor.tsx` | Accepts `onOpenSelector?: () => void`. Renders `<MobileDocTrigger docName={doc}>` at the top when `onOpenSelector` is set. |

### Behaviour

- **Desktop (≥ `md`):** Two-column flex layout. Sidebar always visible on the
  left (`w-72`). Trigger row hidden (`md:hidden`).
- **Mobile (< `md`):** Viewer/editor takes full width. Sidebar hidden
  (`hidden md:flex`). Trigger row visible at the top showing current doc name +
  chevron, or "Choose a document" when no doc is selected. Tapping the trigger
  sets `isSheetOpen = true`. The trigger is also rendered in the "No KB docs
  yet" and "Select a doc." fallback branches, so mobile users always have a way
  to open the sheet regardless of selection state.

### Closing the sheet

The sheet closes on either of:
- Tap the dark backdrop (`onClick` on the `bg-black/50` overlay)
- Select any document (intercepted by `MobileDocSheet.handleSelect`)

### CSS conventions

`MobileDocSheet` uses `card-panel` and `animate-panel-slide-in` — the same
classes used by `CardPanel` — so z-index and animation stay consistent with
the rest of the app. No new CSS was added.

The trigger button uses `style` props (CSS custom properties) rather than
Tailwind color classes, consistent with the project-wide convention.

## Mobile NowRail drawer

On viewports narrower than `768px` the board's right-hand `NowRail` (agents ·
capacity · activity feed) collapses into a slide-in drawer triggered by the
existing **Show rail** button in `BoardFooter`. On desktop the rail is a 280 px
sidecar in the flex row. The rail is hidden by default on every viewport — the
user opens it via the toggle in `BoardFooter`.

### Architecture

| File | Role |
|---|---|
| `web/src/components/Board/Board.tsx` | Derives `isMobile` from `useMediaQuery('(max-width: 768px)')`. The rail starts hidden at every viewport (`useState(false)`); the user opens it via the **Show rail** button. Renders a `.now-rail-backdrop` sibling when `isMobile && nowRailOpen`, and passes `className="animate-panel-slide-in"` to `NowRail` on mobile so the drawer slides in from the right. |
| `web/src/components/Board/NowRail.tsx` | Accepts `className?: string` and merges it onto `<aside class="now-rail">`. The desktop layout never receives a className, so the slide-in animation only runs on the mobile breakpoint. |
| `web/src/index.css` (`@media (max-width: 768px)`) | Switches `.now-rail` to `position: fixed; right: 0; width: min(320px, 88vw); z-index: 50`. Adds `.now-rail-backdrop` (`position: fixed; inset: 0; z-index: 40; background: rgba(0,0,0,0.5)`). Makes `.board-footer` `position: sticky; bottom: 0; z-index: 41` so the rail-toggle stays reachable both above the backdrop and during the page's vertical scroll. |

### Behaviour

- **Desktop (≥ `md`):** Rail is the 280 px sidecar inside the flex row.
  Hidden by default; toggled via the **Hide rail** / **Show rail** button in
  `BoardFooter`.
- **Mobile (< `md`):** Rail is hidden on first mount.
  Tapping the rail toggle opens the drawer over the kanban with a darkened
  backdrop. The backdrop or the rail toggle (now at `z-index: 41`) both
  close it.

### Closing the drawer

- Tap the backdrop (`onClick` calls `setNowRailOpen(false)`)
- Tap the rail toggle in `BoardFooter` again (same handler)
- No Escape-key handler — consistent with `MobileDocSheet` and the mobile
  sidebar; the explicit toggle is the dedicated affordance.

### CSS conventions

The slide-in is the existing `animate-panel-slide-in` class — same animation
used by `MobileDocSheet` and `CardPanel`. The `@media` block only handles
layout shape (`position: fixed`, sizing, z-index, padding); motion stays
keyed off the shared class so future tweaks (e.g. `prefers-reduced-motion`)
apply to all panels at once.

### Test coverage gap

Vitest + jsdom does not apply CSS, so the `@media (max-width: 768px)` block
itself is functionally untested. The Board.test.tsx cases mock `matchMedia`
and assert DOM-level open/close state only — they do **not** verify that
`.now-rail` is `position: fixed` at the mobile breakpoint or that the
backdrop's z-index is below the rail. A future Playwright smoke at
375 × 667 would close this gap; until then, the layout is verified by
manual QA per `docs/superpowers/specs/2026-05-18-board-mobile-design.md`
§ Testing.

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

## ConfirmModal

`web/src/components/ConfirmModal/ConfirmModal.tsx` — reusable themed confirmation
dialog. **Use this instead of `window.confirm()` for any new confirmation flow.**

```ts
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
```

Props:

| Prop | Type | Default | Description |
|---|---|---|---|
| `open` | `boolean` | — | Controls visibility. Renders nothing when `false`. |
| `title` | `string` | — | Dialog heading. |
| `message` | `string \| ReactNode` | — | Body text. |
| `confirmLabel` | `string` | `"Confirm"` | Confirm button label. |
| `cancelLabel` | `string` | `"Cancel"` | Cancel button label. |
| `variant` | `"default" \| "danger"` | `"default"` | `"danger"` renders the confirm button in `--red`/`--bg-red`; use for destructive actions. |
| `onConfirm` | `() => void` | — | Called when user confirms. |
| `onCancel` | `() => void` | — | Called on cancel, Escape, or backdrop click. |

Behaviour: `fixed inset-0 z-50` overlay centered in the viewport; backdrop
`bg-black/50`; uses `useFocusTrap` with initial focus on the Confirm button
(Enter confirms, matching native `confirm()` ergonomics); Escape → `onCancel`;
backdrop click → `onCancel`. CSS custom properties only — works across all
palettes and light/dark modes without any changes.

The Promote-to-Autonomous flow in `CardChat.tsx` is the reference integration.
When migrating the remaining `window.confirm()` calls in the codebase (Delete
button, etc.), follow that pattern: add a `confirmOpen` boolean state, open the
modal on button click, run the action in `onConfirm`, and close in `onCancel`.

## CardPanel destructive actions

Destructive actions live in the **Danger tab** of the card panel's right rail,
rendered by `CardPanel/CardPanelDangerZone.tsx`. The header no longer carries
a Delete button — the move to a dedicated tab keeps destructive UI out of the
primary action row and leaves space for a clearer tooltip / warning copy.

### Delete button

The Delete button in the Danger tab is enabled only when both conditions hold
simultaneously:

- `card.state === 'todo' || card.state === 'not_planned'`
- `!card.assigned_agent`

When either condition fails (e.g. card is `in_progress`, or currently claimed),
the button is rendered but `disabled` and the tab shows a plain-English reason
("An agent has an active claim", "current state is …"). The button is also
disabled while a delete is in flight and shows "Deleting…" as its label.

Clicking the enabled button opens a `<ConfirmModal variant="danger">` warning
that the card file will be `git rm`'d and committed, and the action is
irreversible. Confirming calls `api.deleteCard(project, cardId)`, which issues
`DELETE /api/projects/{project}/cards/{id}`. On success the panel closes and
the card is removed from local board state. A 409 response (card has subtasks
— backend rejects with 422 `VALIDATION_ERROR`) surfaces an error message to
the user and leaves the panel open.

Styling uses CSS variables only: `--red` for the text/border and `--bg-red`
for the background. No hardcoded hex values.

`CardPanelDangerZone.tsx:100-108` is the canonical `ConfirmModal` integration
reference alongside the Promote-to-Autonomous flow in `CardChat.tsx`. When
adding new destructive confirmations, mirror that pattern (a local
`confirm*Open` boolean, open on click, run the action in `onConfirm`, close
in `onCancel`).

## MetricsRibbon and KpiRow — delivery-unit counters

Cards where `parent === ""` are **delivery units** (standalone tasks and parent
tasks). Subtasks are excluded from the headline metrics because they inflate
throughput counts when work is decomposed into fine-grained steps.

### Board MetricsRibbon (`MetricsRibbon.tsx`)

The four affected tiles — **In flight**, **Stalled**, **Shipped today**, and
**Shipped · 7d** — compute their headlines, delta percentages, and sparklines
from the `*_parents` fields in `DashboardData` and `MetricSeries`:

- `state_counts_parents`, `in_flight_parents`, `stalled_parents`, `shipped_parents`
- `cards_completed_today_parents`, `cards_completed_last_7d_parents`,
  `cards_completed_prior_7d_parents`

When subtasks add to the all-cards count (i.e. `total − parents > 0`), the tile
renders a muted secondary figure: `<span class="metric-tile__sub">+N sub</span>`.
CSS for `.metric-tile__sub`: `font-family: var(--font-mono); font-size: 10.5px;
color: var(--grey1);` — mirrors `.metric-tile__delta`. Computed and wired in
`Board.tsx` before being passed as `inFlightSubtasks`, `stalledSubtasks`,
`shippedTodaySubtasks`, `shipped7dSubtasks` props.

The `+N sub` glyph itself is rendered by the **`SubCount`** helper exported
from `MetricsRibbon.tsx`. Other surfaces that need the muted-suffix idiom
should import `SubCount` from there rather than duplicating the JSX or the
`.metric-tile__sub` class wiring. The helper absorbs the guard
(`n === undefined || n <= 0 → null`) so callers only need to compute the
diff.

**Active Agents** is intentionally NOT filtered — an agent on a subtask is real
activity.

Backend source: `internal/service/service_dashboard.go`.

### All Projects KpiRow (`KpiRow.tsx`)

The three affected tiles — **Open tasks**, **In progress**, and **Done today** —
derive from `state_counts_parents` and `cards_completed_today_parents` summed
across projects by `aggregateDashboards` in
`web/src/components/AllProjectsDashboard/utils.ts`.

No `+N sub` figure is shown (All Projects is a glanceable operations view). Each
of the three tiles carries a `title` tooltip: "Counts delivery units (standalone
tasks + parents). Subtasks are excluded."

**Total cost** is intentionally NOT filtered — subtask cost is real money spent.

### Board subheader BoardBand (`BoardBand.tsx`)

The rolling stats line (`N agents live · N open · N in review · N shipped today
· N shipped this week · ±N%`) is **strict parent-only**. `Board.tsx` derives
`openCount` and `inReviewCount` from `state_counts_parents` (falling back to
`cards.filter(c => !c.parent && …)` when parent counts have not loaded yet) and
passes `cards_completed_*_parents` directly for the shipped figures. No `+N
sub` suffix is rendered — the band is a glanceable headline, not a tile, so
the decomposition is left to the MetricsRibbon underneath.

## URL state (query params)

The app uses `useSearchParams` for a small number of bookmarkable / shareable
UI states. All writers pass `{ replace: true }` so the back button isn't
polluted. **Prefer the updater-callback form**
(`setSearchParams((prev) => { … return prev; }, { replace: true })`) so
concurrent writers don't clobber each other's keys. The snapshot form
(`setSearchParams(new URLSearchParams(snapshot), { replace: true })`) exists
in some legacy call sites and is functional but should be migrated when
touched.

| Param | Owner | Route | Form | Meaning |
|---|---|---|---|---|
| `?card=<id>` | `useDeepLinkCard` (`web/src/components/ProjectShell/useDeepLinkCard.ts`) | `/projects/:project` | updater | Opens `CardPanel` for the given card ID on mount; cleared when the panel closes. |
| `?project=<name>` | `TopCardsPanel` (`web/src/components/AllProjectsDashboard/TopCardsPanel.tsx`) | `/`, `/all` | updater | Filters `TopCardsPanel` to one project. Value is the project's `name` (slug), not `display_name`. Unknown / stale slugs are treated as no filter (the dropdown stays on "All projects" and the full top-5 is shown). |
| `?new=1` | `ChatPage.closeDialog` (`web/src/pages/Chat/ChatPage.tsx`) | `/chat` | snapshot (legacy) | Opens the `NewChatDialog` on mount; cleared when the dialog closes. |

When adding a new URL-state param: use `useSearchParams` with the updater
form, validate the value against the known set (or fall back gracefully on
unknown), and add a row to this table.

## 404 / Not Found handling

ContextMatrix is a SPA served by a Go backend that returns `index.html` for all
non-API, non-static paths (see `newSPAHandler` in `cmd/contextmatrix/main.go`).
Unknown URL handling therefore lives entirely in React Router, not the backend.

### Catch-all routes

`<Route path="*" element={<NotFound />} />` is registered as the last route at
**two levels**:

| File | Scope |
|---|---|
| `web/src/App.tsx` | Top-level routes (`/`, `/all`, `/projects/:project/*`) |
| `web/src/components/ProjectShell/ProjectShell.tsx` | Nested project routes (`/`, `/settings`, `/knowledge`) |

Both levels must have the catch-all so that:
- `/unknown-top-level` is caught by `App.tsx`
- `/projects/alpha/unknown-sub-page` is caught by `ProjectShell.tsx`

### NotFound component

`web/src/components/NotFound/NotFound.tsx` — a self-contained 404 page.

- Uses CSS variables only (`--bg0`, `--fg`, `--red`, `--grey1`, `--aqua`,
  `--bg2`, `--bg3`). No hardcoded colours.
- The `404` indicator is `aria-hidden="true"` (decorative); the heading is an
  `h1` for accessibility.
- A `<Link to="/">Go home</Link>` returns the user to the root, which renders
  the All Projects operations dashboard (`AllProjectsDashboard`). `/all` is
  preserved as an alias so existing sidebar links and bookmarks still resolve.
- Exported via `web/src/components/NotFound/index.ts` barrel (standard pattern).

### Adding routes in future

When adding a new top-level route in `App.tsx` or a new nested route in
`ProjectShell.tsx`, always keep the `path="*"` catch-all as the **last** entry.
React Router evaluates routes in declaration order, so inserting a route after
the catch-all has no effect.
