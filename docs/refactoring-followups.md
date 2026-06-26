# Refactoring Follow-ups

Findings from the full-codebase production-readiness review (May 2026) that were
classified as Major but deferred from the fix loop because they require
dedicated refactoring rather than targeted bug fixes. None of these are
correctness or security defects — the build, tests, and lint are clean — but
they exceed the project's documented size guidelines (~150 lines per React
component, ~100 lines per Go function) and are concentrated coupling points that
will resist future change if left as-is.

## Status (2026-05-20)

Refactoring executed under
[`docs/superpowers/plans/2026-05-20-refactoring-followups.md`](superpowers/plans/2026-05-20-refactoring-followups.md).
All structural refactorings below were applied; build, tests, and lint remain
clean on `chore/full-code-review` (44 commits since `838291f`).

**Completed:** every Backend and Frontend item below except where noted as
deferred. Highlights:

- `cmd/contextmatrix/main.go`: 887 → 602 LOC; `wireChat`, `wireGitSync`,
  `wireRunnerSubsystems`, `runShutdownSequence` extracted into sibling
  `wire_*.go` files.
- `internal/mcp/tools.go`: 1516 → 160 LOC; split into `tools_cards.go`,
  `tools_lifecycle.go`, `tools_workflow.go`, `tools_projects.go`.
  `requireActiveClaim` shared between `registerAddLog` and `registerCompleteTask`.
- Service-layer functions (`TransitionTo`, `CreateCard`, `applyCardMutation`,
  `GetDashboard`, `UpdateRunnerStatus`) brought under the 100-LOC function
  budget via named pipeline helpers.
- Chat manager: `ensureRunningForSend`, `coldPrep`, `rollbackContainer`
  extracted.
- Cross-cutting Go: Go 1.26 `new(expr)` adopted (the planned `ptr.Of` helper
  was redundant); `extractAgentID` consolidated; new `ErrCode*` constants
  (`ErrCodeSyncDisabled`, `ErrCodeSyncError`, `ErrCodeNoGitHubRepo`).
- Frontend hooks: `useTimeoutRef`, `useOncePerKeyToast`, `useRunnerHealth`,
  `useDashboardPolling`, `useActivityFeed`, `useCardEdits`, `useRailSync`,
  `useCreateCardForm` — all newly introduced and adopted.
- Frontend components: `BifoldHeader`, `ChipPicker`, `CardChipRow`,
  `ChatComposer`, `CostByModel`, `AgentsOnDuty`, `RemoteExecutionSection`,
  `RepoListSection`, `StateTransitionEditor`, `GitHubImportSection` shells.
  `CardItem` is now `React.memo`-wrapped and consumers use the memoized
  named export.
- Frontend CSS: `.apd-meta-line`, `.cm-header-icon-btn`, `.cm-pal-menu-item`,
  `.cm-chat-row` added; four hover sites migrated from JS handlers to
  `:hover` rules.
- Type-system hygiene: `runnerStatusStyles` moved from `types/index.ts` to
  `lib/chip.ts`.

**Deferred (out of scope for a pure refactor; needs its own brainstorm + plan
each):**

- Refresh registry opaque `JobID` (new API contract).
- Operator-visibility log on chat-runner container drift (new emission).
- `PaletteSelector` ARIA menu pattern (new accessibility behavior).
- `Board.tsx` column virtualisation (perf-driven rendering change).
- `ProjectSettings` `cardCount` swap from `api.getCards` to
  `dashboard.state_counts` (different API path).
- `useDashboardPolling` `document.visibilityState` gate (new runtime
  behavior; hook extraction itself shipped without it).
- `--bg-aqua` palette audit (investigation may add CSS tokens).
- `LogEntryType` strict union for `'usage'` (type / parsing change).
- `isAPIError` exhaustive narrowing (behavior change in branches).
- `sessionlog.Stop` debug-build assertion (the doc-clarification half
  shipped at commit 8bee865; the assertion half is a runtime addition).
- `internal/chat/manager.doClearContext` routing through
  `appendMessageWithKind`: the plan's premise was wrong. `doClearContext`
  uses `store.ClearTranscriptAtomic` (a two-step transactional
  update+insert that flags pre-clear messages with
  `rehydration_phase=true`); `appendMessageWithKind` uses
  `store.AppendMessage` (single insert). Substitution would silently break
  `TestClearContext_HappyPath`'s pre-clear marking invariant. Keep
  `doClearContext` as-is. If the shared seq-assign/rollback ever becomes
  worth deduping, factor a `nextSeq` helper rather than collapsing the two
  callers.

## Backend

### `cmd/contextmatrix/main.go` (~648 lines)

Mixes dependency wiring with embedded business logic:

- Chat reattach loop (`~lines 422-442`).
- Grace-timer setup (`~lines 367-411`).
- Phased shutdown ordering (`~lines 595-708`).

**Suggested extraction:**

- `wireChat(ctx, cfg, runner) (*chat.Manager, *chat.SSEHub, cleanupFn)`
- `wireGitSync(cfg, git, store, svc, bus) *gitsync.Syncer`
- `wireRunnerSubsystems(...)` covering runner client, sessionlog manager,
  reconciler.
- `runShutdownSequence(ctx, components)` for phases 1-5.

After extraction `main()` should be ~150 lines of declarative wiring.

### `internal/service/service_cards.go`

- `CreateCard` (~218 lines): split into `buildNewCardFromInput`,
  `applyDedupGuard`, `commitNewCardWithNextID`, `rollbackCreate`. Reuse the
  `applyCardMutation`-style snapshot/rollback discipline (currently the rollback
  is inline and duplicates the helper's shape).
- `applyCardMutation` (~187 lines): extract
  `publishStateOrUpdate(card, agent, oldState, stateChanged)`,
  `runValidatorsAndDeps(...)`, and `commitAndRollbackOrReturn(...)` helpers.

### `internal/service/service.go`

- `TransitionTo` (~141 lines): the manual `unlocked bool` ratcheting in the
  multi-step path is hard to follow. Replace with a `transitionStep()` helper
  that owns one iteration's lock/release/await cycle.

### `internal/service/service_dashboard.go`

- `GetDashboard` (~222 lines, ~5 independent rollups in one card loop). Split
  into `aggregateCostsByAgentModel`, `bucketCompletions`, `bucketSparkline`,
  `buildAgentList`. The single-pass micro-optimisation doesn't matter for the
  dashboard's call volume.

### `internal/service/service_runner.go`

- `UpdateRunnerStatus` (~152 lines): the round-1 fixer extracted
  `normalizePostTerminalStatus`, `clearAgentOnTerminalRunnerStatus`,
  `runnerStatusEventType`. Round-2 review flags the function is still long;
  further split the message-append branch and the sessionManager lifecycle hooks
  into named helpers.

### `internal/chat/manager.go`

- `openCold` (~229 lines): extract `coldPrep(ctx, sess) (StartChatOpts, error)`
  that gathers `repoURL` / `resume` / `primer` / `model`, and
  `rollbackContainer(reason, sessID, containerID, opErr)` helper to dedupe the
  three `runner.EndChat` + log call sites in the rollback paths.
- `doClearContext` (~104 lines): the seq-assign / rollback dance is hand-coded
  duplicate of `appendMessageWithKind`. Route the divider insert through
  `appendMessageWithKind(ctx, id, RoleSystem, ContextClearedMarker, EventKindDivider)`
  and consolidate the seq-assign / rollback logic in one place.
- `SendUserMessage` (~52 lines): extract
  `ensureRunningForSend(ctx, sess) (Session, error)` for the cold→OpenSession /
  warm-idle→MarkActive / active→pass-through promotion.

### `internal/mcp/tools.go` (~1500 lines)

Split by area for navigability:

- `tools_lifecycle.go` (claim/release/heartbeat/complete/log).
- `tools_workflow.go` (skill/workflow/review tools).
- `tools_projects.go` (project CRUD).
- `tools_cards.go` (create/update/patch/get/list).

`registerTools` stays as the single registration entry point.

Also: the ownership-gate code (load card, check `AssignedAgent == ""`, check
mismatch) is duplicated verbatim across `registerAddLog` and
`registerCompleteTask`. Extract
`requireActiveClaim(ctx, svc, project, cardID, agentID, toolName) (*board.Card, error)`
mirroring `requireHumanAgent` and have both callers share it.

### `internal/runner/sessionlog/manager.go`

- `Stop` (~40 lines, lines 329-369): documents but does not encode an invariant
  that `pendingSubs[cardID]` can grow between `delete(m.activeSessions)` and the
  second lock window. Either move the documentation into the function or add an
  assertion in debug builds.

### Generic patterns to centralise

- **`ptr.Of[T]` helper.** Several handlers and service callers spell
  `localTrue := true; field := &localTrue`. A tiny generic helper
  (`func ptrTo[T any](v T) *T`) removes the boilerplate; mostly affects
  `internal/api/runner.go` and `internal/service/service_cards.go`.
- **Single agent-id reader.** `cards.go`, `chats.go`, `runner.go`, and
  `agents.go` all read `r.Header.Get("X-Agent-ID")` inline with subtly
  different trimming. Route everyone through `extractAgentID` (or a private
  `headerAgentID`).
- **Error code registry.** `branches.go`, `sync.go`, and a handful of others use
  ad-hoc string literals (`"NO_GITHUB_REPO"`, `"SYNC_DISABLED"`, etc.) instead
  of named constants in `router.go`'s central block. Promote to `ErrCode*`
  constants.

## Frontend

### `web/src/components/Board/Board.tsx` (~428 lines)

- Extract
  `deriveMetricsProps(dashboard, cards, stateCounts, stateCountsParents)` into
  `Board/metrics.ts` so the MetricsRibbon derivation block (~lines 254-311) is
  unit-testable in isolation.
- Consider memoising / virtualising large columns. Today each column renders
  every card (`overflow-y-auto`, no virtualization); for projects with hundreds
  of cards in a column this is a perf cliff. Reuse the `VirtualLogList`
  variable-height pattern from the runner console.

### `web/src/components/ProjectShell/ProjectShell.tsx` (~476 lines)

Owns seven `useEffect`s and three feature areas. Extract:

- `useRunnerHealth(REFRESH_INTERVAL)` (lines 97-130).
- `useDashboardPolling(project)` (lines 134-150) — and gate it on
  `document.visibilityState !== 'hidden'` like `useRunnerHealth` already does.
- `useActivityFeed(project, bus)` (lines 163-223), including the backfill + SSE
  merge + dedup.

After extraction `ProjectShell` is wiring + JSX only, ~200 lines.

### `web/src/components/CardPanel/CardPanel.tsx` (~369 lines)

Extract two hooks:

- `useCardEdits(card)` owning `editedCard`, `isDirty`, `handleSave`,
  `handleRun`, `handleTransitionPrimary`, and the optimistic-rollback flags.
- `useRailSync(card, isHITLRunning, isMobile)` owning `sync`, `railExpanded`,
  `activeTab`, the `hitlOffCount` debounce, and the documented two-sync HITL-off
  state machine.

After extraction the component body is plumbing + JSX, ~120 lines.

### `web/src/components/CreateCardPanel/CreateCardPanel.tsx` (~351 lines)

Two issues compound here:

1. Component re-implements its own header (~lines 323-415, ~90 lines) instead of
   reusing `CardPanelHeader`. Extract a `BifoldHeader` shell (`title` / `chips`
   / `actions` props) used by both panels.
2. `ChipPicker` (lines 27-62) duplicates the chip-with-select pattern from
   `CardPanelHeaderChips.tsx`. Promote to a shared component
   (`web/src/components/CardPanel/ChipPicker.tsx`) and consume from both sites.

After extraction the form-state lives in a `useCreateCardForm` hook and the
component is JSX-heavy / logic-light.

### `web/src/components/ProjectSettings/ProjectSettings.tsx` (~548 lines)

- Replace the unbounded `api.getCards(project)` for `cardCount` with the sum of
  `dashboard.state_counts` (or a dedicated count endpoint). The current code
  paginates every card in the project just to disable one button.
- Extract per-section sub-components (e.g. `RemoteExecutionSection`,
  `RepoListSection`, `StateTransitionEditor`) — the file is mostly a flat form
  with a `isDirty` memo, no real composition.

### `web/src/components/AllProjectsDashboard/CostAgentsPanel.tsx` (~457 lines)

- Split the inline `CostByModel` (~95 lines) and `AgentsOnDuty` (~90 lines) into
  sibling files.
- The repeated inline-style cluster (`fontFamily: var(--font-mono)`,
  `fontSize: 11.5`, `color: var(--grey1)`) belongs in `.apd-*` CSS classes
  alongside the existing `.apd-card`, `.apd-tab-strip` rules.
- Use `useRef<Map<Tab, HTMLButtonElement>>` for tab focus instead of
  querySelectoring by id.

### `web/src/components/Board/CardItem.tsx` (~307 lines)

- Collapsed (~lines 97-153) and expanded (~lines 155-307) returns share most of
  the chip JSX. Extract `CardChipRow` with a `compact` flag.
- Wrap `CardItem` in `React.memo` so SSE-driven Board renders don't re-render
  every visible card.

### Smaller frontend follow-ups

- **`AppHeader`/`PaletteSelector`/`ChatSection`/`ThemeToggle` hover.** Move
  inline `onMouseEnter`/`onMouseLeave` style mutations to CSS `:hover` rules.
  The pattern is duplicated in 4 places.
- **`useOnceToast` extraction.** The "show toast once per session, track via
  ref" pattern is duplicated in `AllProjectsDashboard.tsx` (appConfig
  - syncStatus) and similar fetchers. Extract `useOncePerKeyToast(key)`.
- **PaletteSelector ARIA menu pattern.** Add `aria-haspopup`, `aria-expanded`,
  and ArrowDown/Up/Home/End/Enter keyboard navigation. `PaneHeader.tsx:131-133`
  already implements the correct pattern — mirror it.
- **Centralise the "scheduled timer ref" idiom** (`useChatLayout`,
  `ProjectShell.flashTimerRef`, `ChatPage.timerRef`) into a small
  `useTimeoutRef` hook.
- **`ChatPanel` compose textarea lift.** Currently every keystroke re-renders
  the parent panel which re-walks `filteredLogs` for the visible-region scroll
  calculation. Move the textarea + its `message`/`sending`/`error` state into a
  child component so typing doesn't re-render the log column.
- **Verify `--bg-aqua` exists in every palette.** Referenced in `lib/chip.ts`
  and `CostAgentsPanel.tsx`; not listed in `web/CLAUDE.md`'s palette doc. If
  it's only defined for Everforest, Radix and Catppuccin will render broken
  badges silently.

### Type-system / API hygiene

- **Strict `LogEntryType`.** `'usage'` is parsed in `useRunnerLogs` but not in
  the `LogEntryType` union (`web/src/types/index.ts:323`). Either add it or
  document the omission.
- **`runnerStatusStyles` belongs in `lib/chip.ts`.** Currently it's the only
  runtime export from `types/index.ts`; move it to keep `types/` type-only.
- **`isAPIError` strictness.** The Round-1 fixer added a string-type check on
  `.error`; ensure the type-narrowing exhausts (`code?: string` should also be
  checked when callers branch on it).

## Cross-cutting / process

- **Refresh registry: opaque `JobID`.** Round-2 Specialist 5A suggested
  attaching an opaque `JobID` to each `Acquire` and requiring it on every
  subsequent mutator (`MarkRunning`, `UpdateProgress`, `MarkTerminal`). This
  closes a class of "ghost progress" bugs where a late callback against a
  superseded job mutates the live one.
- **Operator visibility for chat-runner container drift.** When
  `SendChatMessage` fails right after a cold-open started a container, the
  runner container is up but the user gets a 5xx. The reaper / warm-idle TTL
  eventually reaps; until then, log a clear "container started but first message
  failed" warning so operators can correlate.

## Notes on file-size targets

The project's `web/CLAUDE.md` documents a ~150-line component size limit and the
Go side has an implicit ~100-line function limit (called out in several review
prompts). The list above flags files that exceed these budgets by 2× or more.
Smaller infractions (200-line files, 110-line functions) were not enumerated —
handle them opportunistically when touching the surrounding code.
