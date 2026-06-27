# Refactoring Follow-ups

Open structural follow-ups from the full-codebase production-readiness review.
None are correctness or security defects — the build, tests, and lint are clean.
Each needs its own brainstorm and plan before it is picked up.

## Backend

- **`sessionlog.Stop` debug-build assertion**
  (`internal/runner/sessionlog/manager.go`). `Stop` documents but does not encode
  the invariant that `pendingSubs[cardID]` can grow between
  `delete(m.activeSessions)` and the second lock window. Add a debug-build
  assertion that enforces it.

## Frontend

- **`Board.tsx` column virtualisation.** Each column renders every card
  (`overflow-y-auto`, no virtualization); a column holding hundreds of cards hits
  a perf cliff. Reuse the `VirtualLogList` variable-height pattern from the runner
  console.
- **`ProjectSettings` `cardCount` source.** The delete-guard count comes from an
  unbounded `api.getCards(project)` that paginates every card in the project just
  to disable one button. Replace it with the sum of `dashboard.state_counts` (or a
  dedicated count endpoint).
- **`useDashboardPolling` visibility gate.** Gate the poll on
  `document.visibilityState !== 'hidden'` — as `useRunnerHealth` already does — so
  a backgrounded tab stops polling.
- **`PaletteSelector` ARIA menu pattern.** The selector sets `role="menu"` /
  `role="menuitem"` but lacks `aria-haspopup`, `aria-expanded`, and
  ArrowDown/Up/Home/End/Enter keyboard navigation. `PaneHeader.tsx` implements the
  full pattern — mirror it.

## Cross-cutting

- **Operator visibility for chat-runner container drift.** When `SendChatMessage`
  fails right after a cold-open has started a container, the container is up but
  the user gets a 5xx. The reaper / warm-idle TTL eventually reaps it; until then,
  emit a clear "container started but first message failed" warning so operators
  can correlate the orphaned container with the failed send.

## Known non-refactors

- **`internal/chat/manager.doClearContext` stays separate from
  `appendMessageWithKind`.** `doClearContext` uses `store.ClearTranscriptAtomic`
  — a transactional update+insert that flags pre-clear messages with
  `rehydration_phase=true` — while `appendMessageWithKind` uses
  `store.AppendMessage` (single insert). Routing one through the other breaks
  `TestClearContext_HappyPath`'s pre-clear marking invariant. If the shared
  seq-assign / rollback ever becomes worth deduping, factor a `nextSeq` helper
  rather than collapsing the two callers.

## File-size targets

`web/CLAUDE.md` documents a ~150-line component limit; the Go side carries an
implicit ~100-line function limit. Handle smaller infractions (200-line files,
110-line functions) opportunistically when touching the surrounding code.
