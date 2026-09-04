# Gotchas

Developer traps in this codebase. Each entry names the code it describes.

## Boards, git, and sync

- **Card frontmatter parsing** (`board.ParseCard`): the file must start with
  `---\n`; the closing delimiter is the first `\n---` after it (`bytes.Cut`),
  so a literal `---` inside a quoted YAML value or the body does not split the
  document. `\r\n` is normalised to `\n` first and files over `maxCardSize`
  are rejected before parsing. Do not replace this with a naive split on
  `---`.
- **Shared repos never rebase.** `Syncer.Synced` uses `merge --ff-only` then
  `merge --no-ff` (`internal/gitops/merge.go`). A rebase after a self-healed
  merge would drop the merge commit and re-conflict. Do not "simplify" the
  shared path back to rebase.
- **`CardService.LockWrites(repo)` takes `writeMu`, then quiesces that repo's
  commit queue.** On a shared repo it calls `CommitQueue.Drain` (30 s budget),
  not merely `Pause` + `AwaitIdle`: a buffered job that has not run yet is a
  dirty worktree, and a dirty worktree at merge time means autostash, which
  the shared path refuses. `commitLeftovers` is the backstop, not the design.
  Before it runs, the cycle aborts a merge a crash left in progress
  (`clearStaleMerge`); staging that merge's conflict markers as
  `external edit` would silently conclude it. On a private repo the pause
  plus `AwaitIdle` keeps an in-flight go-git commit from colliding with the
  shell-git rebase on `.git/index.lock`. `UnlockWrites` must `Resume()` before
  releasing `writeMu`; the reverse order leaves the queue paused under a fresh
  write.
- **One `writeMu`, one queue per boards repo.** `LockWrites(repo)` takes the
  single write mutex and quiesces only that repo's queue. A cycle of repo A
  blocks writes to repo B for its duration (bounded by the 10 s sync
  timeouts) and never touches B's queue. Do not add a per-repo mutex without
  re-proving the lock order (playbooks, then `writeMu`, then the one queue);
  do not drain every queue, or a cycle of A waits on B's commits.
- **Resolver order is load-bearing:** `.board.yaml` first (so `MintID` sees
  the merged `next_id`), add/add cards next (so renames exist), then cards,
  then playbooks, then everything else. `resolveRank` in
  `internal/gitsync/resolve.go` encodes it.
- **Reference rewrite after a re-mint** (`rewriteLocalRefs`) touches only the
  re-minted files plus files our side changed since the merge base that the
  merge did not conflict on. A reference to the old ID in a file only the
  remote changed refers to the remote card and must stay. A file both sides
  edited in non-overlapping regions auto-merges and reaches the rewrite too,
  so a reference the remote added there to a colliding ID can be rewritten
  to the local re-mint when it should have kept pointing at the remote card.
- **A `Synced` body runs with the commit queue paused.** It must commit
  through its `DirectCommit` path (`CardService.commitNow` for cards,
  `PlaybookService.directCommit` for playbooks, both built by
  `DirectCommitter`, shell git), never `enqueueCardCommit`: a queued job
  waits for a resume that only comes after the cycle returns. It must not
  take `writeMu` or the playbook lock (the cycle holds both) and must not
  touch the network. `runVerified` wraps every push-verified card, project,
  and stall mutation; `PlaybookService.createVerified` is the one other body.
- **Undo never bumps the epoch** (`undoClaimWrite`). A verified write whose
  push never lands is reverted by restoring the pre-write tuple, epoch
  included, so a peer's claim made in the meantime outranks the reverted
  state in the next merge. An undo that raised the epoch would win that merge
  and evict the peer.
- **Two instances running one card present the same agent ID.** The agent
  backend derives the ID from the card ID. Every ownership check must go
  through `CardService.OwnsClaim` or `board.Card.ClaimHeldBy`; a bare
  `AssignedAgent == agentID` comparison is a bug on a shared board.
- **Heartbeats are not on disk.** `lock.Manager.LastBeat` is the liveness
  accessor; `FindStalled`, the dashboard, `check_agent_health`, and every card
  read go through it. Reading `card.LastHeartbeat` from the store sees a
  value up to `lease_interval` old and misjudges a live claim.
- **Foreign stall compares local time with local time.** `ObserveLeases`
  records when a peer's lease value was first seen on this clock; expiry is
  measured from that, never from the peer's timestamp. Clock skew cannot
  stall a live card, and a laptop that cannot push loses its claims after
  `lease_timeout` even while its agent is alive.
- **`claim.lost` is not a terminal state.** The end-session subscriber kills
  the local container on it directly; the backend's kill callback that
  follows is ignored by `UpdateWorkerStatus` because the card is now claimed
  via another instance. A card a peer deleted under a running container is
  left to the reconcile sweep's missing-card rule.
- **`repoOf(project)` never returns nil.** An unknown project resolves to the
  first configured repo so the store call that follows fails with
  `ErrProjectNotFound`. A write path that needs a repo name (project or
  playbook creation) goes through `repoNamed`, the only source of
  `ErrUnknownBoardsRepo`.
- **A new project must be saved with `SaveProjectIn`.** `Composite.SaveProject`
  only updates a project some repo already owns; a create that calls it lands
  in `ErrProjectNotFound`. `CardService.saveNewProject` picks the right call.
- **`boards_repo` is a read-side stamp.** `ProjectConfig.BoardsRepo` has
  `yaml:"-"`; the composite sets it on every read and it never reaches
  `.board.yaml`. A test comparing a config read through the composite with one
  read from disk must ignore it.
- **`CONTEXTMATRIX_BOARDS_*` and the list form do not mix.** With two or more
  entries any of those variables is a load error, on purpose: an override
  that silently landed on one entry of many would be invisible.
- **`gitops.CommitQueue` per-project ordering, idle teardown:** one goroutine
  per project serialises that project's commits in enqueue order; different
  projects commit in parallel. `cmd/contextmatrix/wire_repos.go` wires
  `WithIdleTimeout(30*time.Minute)`, so an idle worker exits and the next
  `Enqueue` spawns a fresh one; `projectWorker.closed` plus the per-worker
  mutex stop an `Enqueue` from sending into a channel the worker is about to
  abandon. Do not assume worker identity across an idle gap.
- **Network git calls are bounded by `gitops.NetworkGitTimeout` (120 s) and
  every `runGit` sets `cmd.WaitDelay = 3 * time.Second`.** Fetch, `pull
  --rebase`, `pull --ff-only`, push, and clone run under that timeout so a
  black-holed remote cannot wedge the board while `pullRebase` /
  `pushWithRetry` hold `LockWrites`. Local calls (`rebase`, `isBehind`,
  `status`) are deliberately unwrapped. The timeout alone is not enough:
  `runGit` gives `cmd.Stdout`/`cmd.Stderr` a `bytes.Buffer`, so `os/exec`
  wires OS pipes and `cmd.Run()` blocks until every holder of the write end
  exits. Over HTTPS, git spawns a `git-remote-https` child that inherits the
  pipes, and `exec.CommandContext` kills only the direct `git` process, so
  without `WaitDelay` the orphaned helper keeps `cmd.Run()` blocked and
  `writeMu` held. Keep new network calls inside the wrapper and keep the
  `WaitDelay`.
- **Deferred git commits (`boards.git_deferred_commit`):** agent mutations
  (heartbeats, log entries, intermediate updates) are batched and committed
  in one flush at release or completion. Two mutations always commit
  immediately regardless: card creation (card file and `.board.yaml`
  together, so the card survives a pull elsewhere), and human edits to
  unclaimed cards via REST (the PUT/PATCH handlers set `ImmediateCommit` when
  `AssignedAgent == ""`). MCP tool callers never set that flag.

## MCP and HTTP server

- **MCP card results are summaries by design:** mutation and list tools
  return `CardSummary` (no `body`, no `activity_log`); `heartbeat` returns a
  three-field ack. Returning the full `board.Card` multiplies agent context
  cost (bodies grow during a run and every result is re-read on each model
  call) and the summary shape is what keeps unvetted external bodies out of
  mutation results. Field parity is enforced by
  `TestCardSummaryMirrorsBoardCard`; the wire contract the agent parses is
  pinned by `TestSlimToolResultsOmitBodyAndActivityLog`. Full cards come
  only from `get_card` / `get_task_context`, and even there the siblings
  array is summaries (`TestGetTaskContextSiblingsAreSummaries`). Skill
  prompts are filtered the same way: review-task, document-task, and the
  execute-task parent inject only the intro plus allowlisted sections
  (`filterBodySections`, `internal/mcp/bodysections.go`; both keep lists
  include `## Decisions`), pinned by
  `TestStartReview_BodyFilteredToPlanAndFindings` and friends. Do not pass a
  nil section list: the early-run builders (create-plan, plan-draft,
  run-autonomous, systematic-debugging) are the only deliberate full-body
  injections (`TestGetSkill_CreatePlan_FullBodyPinned`).
- **MCP middleware chain and body limit:** `/mcp` is registered on the same
  inner mux as the REST API, so it inherits the shared chain: recovery,
  security headers, CORS (when configured), request ID, observe, body limit,
  `csrfGuard`. The cap is 5 MB (`maxRequestBodySize`) on every route unless
  registered through `bodyLimitOverrides`, which may only raise it
  (`POST /api/images` gets 11 MiB). A `Content-Length` over the cap is
  rejected with `413` before the body is read; bodies without one are capped
  via `http.MaxBytesReader`.
- **SSE and MCP streaming vs `WriteTimeout`:** `http.Server.WriteTimeout`
  (60 s) is an absolute deadline from when headers are read; intermediate
  writes do not reset it, so every long-lived stream would hit it. The fix is
  `http.NewResponseController(w).SetWriteDeadline(time.Time{})` before the
  streaming loop, applied in `internal/api/events.go`,
  `internal/api/worker_logs.go`, `internal/api/chats.go` (chat stream), and
  the `clearWriteDeadlineForStreaming` middleware in
  `internal/mcp/server.go` for the MCP `GET` stream. `DELETE` and `POST` keep
  the timeout, except the `await_subtasks` tool call, which blocks for
  minutes by design: `mcpRequestInfoMiddleware` clears the deadline for it
  after sniffing the tool name out of the JSON-RPC body. **Critical:**
  `ResponseController` reaches the connection by calling `Unwrap()` on the
  writer. Any middleware that wraps the writer must implement
  `Unwrap() http.ResponseWriter` or `SetWriteDeadline` fails silently and the
  timeout stays active.
- **Proxy idle timeouts cut `await_subtasks` before `await_max` does.**
  Anything in front of CM (Cloudflare at about 100 s, nginx
  `proxy_read_timeout`, an ingress, a tunnel) applies its own idle timeout,
  and a blocking wait sends nothing until it resolves. The symptom is a
  transport error instead of a `timed_out: true` result. Set `await_max`
  below the shortest idle timeout on the path (`"55s"` behind Cloudflare);
  callers re-call on timeout, so a short cap costs a round trip, not
  correctness. The tool emits MCP progress notifications every 30 s when the
  client supplies a progress token; a client without one gets no keepalive,
  so never rely on that in place of the cap.
- **stdlib URL params:** use `r.PathValue("project")`; route patterns use
  `{project}` syntax (`mux.HandleFunc("GET /api/projects/{project}", h)`).
- **`time.Duration` in YAML:** `gopkg.in/yaml.v3` does not unmarshal
  `"30m"` into `time.Duration`. Config stores durations as strings and parses
  them with `time.ParseDuration` at load.
- **`/healthz` and `/readyz` are not logged:** the request-logging middleware
  skips them so probe traffic does not spam logs. Both still respond
  normally.
- **`request_id` log correlation:** the `requestID` middleware stores a UUID
  in the context via `ctxlog.WithRequestID`. Every log site must use
  `ctxlog.Logger(ctx)`, not `slog.Default()` or a package logger, or the line
  loses the ID. Background goroutines (stall scanner, git-pull ticker) fall
  back to `slog.Default()` safely.
- **MCP tool name in the request log line:** for `POST /mcp` the `observe`
  middleware adds `mcp_method` (JSON-RPC method) and `mcp_tool` (tool name
  for `tools/call`). `mcpRequestInfoMiddleware` reads the body, parses the
  envelope, restores the body, and writes into a `*ctxlog.MCPCall` stashed
  by `observe`; extraction errors are swallowed and the line is still
  emitted.
- **`/metrics` and pprof live on the admin port:** served only on the admin
  listener (`admin_port`, bound to `admin_bind_addr`, default `127.0.0.1`),
  never on the main listener, with no authentication. Keep it loopback-only
  or gate it with a firewall or NetworkPolicy; a non-loopback bind logs a
  warning at startup.

## GitHub

- **Both auth modes are GitHub-only.** `github.auth_mode` is `app` or `pat`;
  there is no other transport. A fine-grained PAT needs
  `Contents: Read and write` on the boards repo and `Issues: Read` on each
  project repo whose `.board.yaml` sets `github.import_issues: true`. See
  [github-auth-setup.md](github-auth-setup.md).
- **All git remote URLs must be HTTPS:** `boards.git_remote_url` and
  `task_skills.git_remote_url` are validated at startup and must start with
  `https://`. SSH URLs are rejected unconditionally.

## Frontend

- **Tailwind v4 preflight strips `list-style` from `ul`/`ol`:**
  `@import "tailwindcss"` injects `@layer base { ol, ul { list-style: none } }`.
  `@uiw/react-markdown-preview` sets `list-style-type` only on nested levels
  and relies on UA defaults for the top level, so bullets vanish.
  `web/src/index.css` restores them with `!important` rules scoped to
  `.wmde-markdown` and re-asserts the nested cascade (`circle`, `square`,
  `lower-roman`, `lower-alpha`), because the base `!important` would
  otherwise beat the library's non-`!important` nested rules.
- **Frontend embed:** `//go:embed all:dist` in `web/embed.go`. The `all:`
  prefix includes dotfiles under `dist/`; a plain glob would miss them. Build
  the frontend before the Go binary (`make test` stubs `web/dist` for test
  builds).
- **404 handling is React Router's job:** `newSPAHandler` returns
  `index.html` for every path that is not `/api/`, `/healthz`, `/readyz`,
  `/mcp`, or a real static file. Unknown routes are caught by
  `<Route path="*" element={<NotFound />} />` as the last route in both
  `App.tsx` and `ProjectShell.tsx`. A new `Routes` subtree needs its own
  catch-all or users see a blank screen.
- **Firefox per-origin SSE connection limit:** Firefox aborts in-flight
  requests to the same origin (`NS_BINDING_ABORTED`) when a new stream pushes
  the total past its limit; Chrome does not. The app therefore shares one
  `EventSource('/api/events')` through `SSEProvider`
  (`web/src/hooks/useSSEBus.tsx`) and fans events out in-process. Never open
  more than one `EventSource` per distinct URL. For worker logs,
  `ProjectShell` owns both `useWorkerLogs` calls: a project-scoped one for
  the console and a card-scoped one enabled while the selected card's
  `worker_status` is `running`, whose entries it passes down as a prop.
  `CardChat` opens no `EventSource` of its own.
- **`useChatStream` ring buffer + REST bootstrap seam:** the hook keeps a
  2000-entry `useRingBuffer` and pairs the SSE subscription with
  `GET /api/chats/{id}/messages?since_seq=0`. On mount or session change it
  fetches the persisted transcript, records the highest `seq`, then
  subscribes with `since_seq=<last>`; SSE events inside the bootstrap window
  are deduped. SSE-only would lose everything older than the server-side
  128-entry ring on refresh.

## Session logs and chat

- **`sessionlog.Manager` fan-out invariants:** one pump,
  `readUpstreamStream`, serves both card-scoped (`Subscribe`) and
  project-scoped (`SubscribeProject`) sessions; project sessions use the key
  `"project:<name>"` in the shared maps so they cannot collide with card IDs,
  and their pump keeps every event's `CardID`. Appending to the ring buffer
  and fanning out happen under one `m.mu` lock; separating them reintroduces
  duplicate delivery (an event in the snapshot and in `sub.pending`). The
  primed-flag protocol enforces snapshot-before-live: the pump stages live
  events in `sub.pending` while `sub.primed` is false, and the snapshot
  goroutine flips `primed` under `m.mu` only after draining both the snapshot
  and `sub.pending` into the channel. Two channels enforce lifecycle safety:
  `done` (closed by `unsub` or `Stop`, guarded by `doneOnce`) tells the
  snapshot goroutine to exit; `snapDone` (closed by that goroutine on exit)
  lets `closeSubscriber` wait up to 1 s before sending the terminal event and
  closing the channel. Closing the channel while the snapshot goroutine may
  still send panics. The snapshot goroutine blocks on each send
  (`select { case ch <- evt: case <-sub.done: return }`) rather than
  dropping, so slow subscribers get the full snapshot.
- **SQLite driver is `modernc.org/sqlite`; pragmas live in the DSN.**
  `internal/sqliteutil.Open` opens every store with
  `file:<path>?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)`
  (plus `foreign_keys(1)` when requested), `MaxOpenConns=5`, `MaxIdleConns=2`.
  The driver name is `"sqlite"`, not `"sqlite3"` (that is `mattn/go-sqlite3`,
  which is not imported so the binary stays CGO-free). `_pragma=` is a
  `modernc.org` extension; switching drivers means `PRAGMA` statements on the
  open connection instead.
- **Chat writes are serialised by `chat.Manager.mu`, not the pool.** SQLite is
  single-writer regardless of `MaxOpenConns`; the pool only lets readers
  (`ListMessages`, `MaxSeq`, `GetSession`) avoid queuing behind a writer.
  `AppendMessage` holds the manager lock across seq assignment and the store
  insert so disk order matches seq order and the in-memory seq cache stays
  consistent with the `(session_id, seq)` UNIQUE index. Raising writer
  concurrency means changing the locking model, not the pool.
- **Chat SSE per-session subscriber cap:** `SSEHub.Subscribe` errors once a
  session has 32 live subscribers (`maxSubscribersPerSession` in
  `internal/chat/sse.go`). The replay ring is 128 events per session
  (`chat.NewSSEHub(128)` in `cmd/contextmatrix/wire_chat.go`); reconnects past
  the window rely on the REST bootstrap in `useChatStream`. Only `message`
  events enter the ring; `session_updated` events are not stored because
  session metadata is re-fetched on reconnect.
- **Op-store schema has no migration ledger.** `ensureSchema` in
  `internal/opstore/sqlite/schema.go` runs idempotent
  `CREATE TABLE IF NOT EXISTS` DDL for `model_blacklist`, `chat_sessions`,
  `chat_messages`, `chat_cost_archive`, and `model_outcomes` in `ops.db`.
  There is no `schema_migrations` table and no column-add helper. A schema
  change is an edit to that DDL; an incompatible `ops.db` is deleted and
  recreated by the operator.
- **Chat rehydration seeds from `resume.jsonl`:** the chat-start payload
  carries `resume` (`ChatResumeContext`), which the chat backend writes to
  the session run dir and the worker reads in-process from
  `/run/cm-chat/resume.jsonl` before its first turn. The transcript is never
  replayed through the container's stdin.
- **`rehydration_phase` stamping prevents reopen pollution:** every message
  appended while `Session.RehydrationActive` is true is stored with
  `rehydration_phase=TRUE` in `chat_messages`. `transcript.Build` drops those
  rows when assembling the next resume payload, so a resumed agent sees only
  real conversation turns plus the prior `chat_rehydration_complete`
  summaries, never earlier agents' tool chatter.
