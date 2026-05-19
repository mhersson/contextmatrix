# Architecture

## Trust model

ContextMatrix is **single-tenant and unauthenticated by design**. There are no
user accounts, no logins, no per-user permissions, no session tokens. Deployment
assumes loopback or a trusted-network ACL (firewall, NetworkPolicy, service-mesh
rule) — same posture as the admin/debug listener documented in
`docs/api-reference.md`.

The implications for code review:

**Identity is not authentication.** The `X-Agent-ID` header tags writes for
audit purposes — boards-repo commit author, activity-log entries,
`assigned_agent` on cards. It is treated like `git config user.name` on a
personal machine: useful for blame, trivial to spoof, and that's fine because
there is no permission gradient to escalate into.

**The web UI auto-generates a per-browser identity.**
`web/src/hooks/useAgentId.ts` mints `human:web-<8 hex chars>` on first visit,
persists it in localStorage, and wires it into every API request via
`api.setAgentId`. We do **not** prompt users for usernames — there is nothing to
authenticate them against, so a prompt is theatre. Per-browser uniqueness
prevents two tabs/users from accidentally releasing each other's card claims;
that is the only reason a unique-per-browser id is needed.

**REST writes that require an identity but receive none fall back to a marker
identity.** `internal/api/knowledge.go` falls back to `human:web` for KB PUT
when `X-Agent-ID` is absent; `internal/api/runner.go` falls back to `human:api`
for the human-only runner endpoints. These markers are honest ("this came from
the web UI / direct API call by an unspecified human") and they preserve write
functionality without inventing fake usernames.

**Where identity gates do exist, they enforce workflow contracts, not access
control:**

- **Card claim / heartbeat / release**: the supplied `X-Agent-ID` must match
  `assigned_agent`. This stops two agents from accidentally clobbering each
  other's claim — it does not stop a malicious caller (who can simply send the
  matching value).
- **MCP human-only tools** (`promote_to_autonomous`, `refresh_knowledge_base`,
  `commit_knowledge_docs`): the `agent_id` argument must start with `human:`.
  The check rejects callers that follow the agent convention of using a
  non-human identifier (e.g. `agent-foo`); a malicious caller can pass
  `human:anything` and the gate yields. The intent is to encode "this operation
  is part of the human workflow," not to prevent forgery.
- **Human-only operations on cards** (e.g. flipping `autonomous: true` via
  `PromoteToAutonomous` / the `promote_to_autonomous` MCP tool): the same
  `human:` prefix check, same intent. The REST handler in
  `internal/api/runner.go` falls back to `human:api` when `X-Agent-ID` is absent
  so the service-layer gate still passes for direct API calls.

**Where real authentication does exist:**

- **MCP Bearer token** (`mcp_api_key` config) for clients connecting over the
  network. Optional for loopback deployments.
- **Runner webhook HMAC** (shared secret in config) for the
  `contextmatrix-runner` callback into the server. The runner is on a different
  host; the secret prevents arbitrary network callers from injecting status
  updates.
- **GitHub authentication** via the shared
  `github.com/mhersson/contextmatrix-githubauth` module (App or PAT). Real auth
  against an external system; do not weaken or bypass.

**What to do during a code review:**

- Treat any "missing X-Agent-ID is a security hole" or "fabricated human:web is
  identity spoofing" finding as **out of scope** — it's the documented trust
  model. If the deployment posture is wrong (CM exposed publicly without a
  network gate), that's an ops concern, not a code-fix concern.
- The MCP human-only checks are workflow gates; do not propose tightening them
  to "real" auth without changing the trust model first.
- The browser-generated agent ID is intentional. Do not propose adding a
  username prompt, OAuth, session cookies, or per-user permissions; those belong
  to a future multi-tenant CM, not this one.
- `githubauth` is the one place where real authentication matters.
  Token-handling code there should be reviewed strictly.

## Data flow

Every HTTP request walks the middleware chain defined in
`internal/api/router.go`:

```text
recovery → securityHeaders → [cors] → requestID → observe → bodyLimit → csrfGuard → mux
```

`recovery` catches panics, `securityHeaders` sets the static security headers
and CSP, `cors` (only registered when `cors_origin` is non-empty) emits the CORS
preamble, `requestID` mints or accepts an `X-Request-ID` and stashes a
request-scoped `*slog.Logger` in context via `ctxlog.WithRequestID`, `observe`
records RED metrics + emits the per-request log line, `bodyLimit` caps inbound
bodies at 5 MB, and `csrfGuard` rejects state-changing requests that lack
`X-Requested-With: contextmatrix` (with narrow exemptions: GET/HEAD/OPTIONS,
`/healthz`, `/readyz`, `/api/runner/*`, and `/mcp`).

Card mutations follow the same pipeline through the service layer:

```text
API handler (deserialize, validate)
  → CardService.<Mutation>
    → writeMu.Lock()
    → Validator.ValidateCard()    — type, state, priority checks
    → Store.UpdateCard()/CreateCard()
                                  — write .md file under storage's writeMu,
                                    update in-memory index
    → enqueueCardCommit(...)      — push gitops.CommitJob on the per-project
                                    queue (or run inline when no queue is wired)
    → writeMu.Unlock()
    → awaitCommit(...)            — block on the queue result without holding
                                    writeMu, so other writers don't stall
    → events.Bus.Publish()        — notify SSE subscribers
  ← return card
← serialize response
```

The MCP server follows the same path — it calls `CardService` methods, never the
store or git layer directly. The `/mcp` handler is registered on the same inner
`http.ServeMux` as the REST API, so MCP traffic shares every middleware listed
above plus an inner stack
(`mcpAuthMiddleware → clearWriteDeadlineForStreaming → chatSessionHeaderMiddleware → mcpRequestInfoMiddleware → SDK handler`).

## Async-commit consistency

Card mutations take an eager-write, async-commit shape:

1. `store.Update*` writes the new card state to the in-memory cache and to disk
   under `writeMu`.
2. The git commit is enqueued via `gitops.CommitQueue.Enqueue` (when a queue is
   wired; otherwise executed inline) and awaited **after** `writeMu` is released
   so slow go-git operations do not block concurrent writers.

`gitops.CommitQueue` runs one worker goroutine per project; commits for the same
project execute strictly in enqueue order, but different projects commit in
parallel. Workers are spawned lazily on first enqueue, and (when constructed
with `WithIdleTimeout`) tear themselves down after a configurable idle window —
`main.go` wires the production queue with a 30-minute idle timeout so long-quiet
projects free their goroutine. The queue exposes `Pause` / `AwaitIdle` so the
gitsync layer can drain in-flight commits before running a shell rebase or push;
`CardService.LockWrites` calls these in sequence.

This means cache + disk can be ahead of git for the window between store write
and commit completion. The service layer closes that gap on failure:

- **Commit success (typical path):** all three substrates (cache, disk, git)
  converge and the caller sees the new card.
- **Commit failure:** `applyCardMutation`, `DeleteCard`, `AddLogEntry`,
  `ClaimCard`, `ReleaseCard`, `markCardStalled`, `RecordPush`,
  `IncrementReviewAttempts`, `UpdateRunnerStatus`, `PromoteToAutonomous`, and
  `ReportUsage` snapshot the pre-mutation card via `store.GetCard` (which
  returns a deep copy) before mutating, then reapply that snapshot via
  `store.UpdateCard` (or `store.CreateCard` for `DeleteCard`) after a failed
  commit. The caller receives `fmt.Errorf("git commit: %w", err)` — equivalent
  to the pre-async behaviour.
- **Rollback failure (rare):** cache and disk become inconsistent with each
  other. A `slog.Error` line carrying `committed=false`, `rollback_failed=true`,
  the card ID, and both errors is emitted for operators; the returned error is
  the `errors.Join` of the original commit error (wrapped with "rollback failed,
  state inconsistent") and the rollback error. The
  `contextmatrix_rollback_failures_total` counter increments on every such
  event. **Alerting:** page on any non-zero rate — each increment is a
  data-integrity event that leaves the named card's cache + on-disk state
  diverged and requires manual reconciliation (typically: inspect the error log
  for the card ID, then re-run the mutation or restore from the git HEAD copy).
- **Heartbeats are a deliberate exception:** `HeartbeatCard` does not roll back.
  A failed heartbeat commit is self-healing — the next heartbeat (typically
  within the heartbeat interval) produces another commit and restores
  consistency.
- **Parent auto-transitions are a deliberate exception:** they are
  fire-and-forget from the child write path (`maybeTransitionParent` →
  `transitionParentDirect`). A failed commit increments
  `contextmatrix_parent_autotransition_errors_total` and logs a Warn; the next
  parent mutation re-commits the state.

## Component responsibilities

- **Store** (`storage.FilesystemStore`): reads/writes `.md` files and
  `.board.yaml` to disk. Maintains an in-memory index. No knowledge of git,
  events, or locking.
- **gitops.Manager** (`gitops.Manager`): stages and commits files, handles
  push/pull with remote repositories. No knowledge of cards or events.
- **Lock Manager** (`lock.Manager`): enforces claim/release/heartbeat rules.
  Reads cards via the store to check ownership but does not write — it returns
  modified card data to the caller (the service layer).
- **Event Bus** (`events.Bus`): in-process pub/sub. Receives events, fans out to
  subscribers.
- **Validator** (`board.Validator`): validates transitions and card fields. Pure
  functions, no side effects.
- **CardService** (`service.CardService`): the only component that orchestrates
  multi-step operations. Every mutation follows: validate → store write → git
  commit → event publish. Also runs the heartbeat timeout checker goroutine,
  which lives here (not in the lock manager) because it coordinates store, git,
  and events.
- **Session Log Manager** (`runner/sessionlog.Manager`): server-side per-card
  SSE buffer and fan-out hub. Keeps a single long-lived authenticated upstream
  connection to the runner per active card, tees events into a bounded ring
  buffer, and replays the buffer snapshot to every new subscriber before tailing
  live events. Started by `CardService.UpdateRunnerStatus` on `→running`,
  stopped (fire-and-forget) on terminal statuses. See `docs/remote-execution.md`
  § Session Log Manager for full details.
- **chat.Manager** (`chat.Manager`): orchestrates the global chat surface
  (project-agnostic chat sessions that share the runner's worker image but use
  long-lived containers instead of card-scoped one-shots). Owns session
  lifecycle (`cold` → `active` → `warm-idle` → `ending`), persists the
  transcript through `chat.Store`, delegates container management to a
  `RunnerClient` (HMAC-signed calls to the runner's `/chat/start` and
  `/chat/end`), and bridges the runner's `/logs?session_id=` SSE feed back into
  the transcript by appending each entry through `AppendMessage`. Holds `m.mu`
  across the seq-assignment + store insert so disk insertion order matches seq
  order regardless of writer concurrency. On cold-reopen,
  `chat.transcript.Build` produces the resume payload shipped to the runner;
  while `RehydrationActive` is true on the session row, `AppendMessage` stamps
  incoming entries with `rehydration_phase=TRUE` so the next reopen can filter
  them out. The MCP tool `chat_rehydration_complete` flips the flag back to
  false and persists the agent's summary as the first visible message. The
  `chat_rehydration_complete` MCP tool is gated by the calling container's
  `CM_CHAT_SESSION` (forwarded as `X-CM-Chat-Session`): a caller can only flip
  its own session's rehydration flag.
- **chat.Transcript** (`chat/transcript`): pure transcript-shaping function — no
  I/O, no state. `Build(messages, opts)` filters out `rehydration_phase=TRUE`
  entries, drops non-conversation roles (stderr, tool results), pins the first
  user turn and the last 20 turns, and truncates middle turns to fit
  `chat.resume_budget_tokens` (default 40k). Returns the kept rows plus a `Meta`
  describing whether the budget clipped older content. Called only on
  `/api/chats/{id}/open` when the session has prior messages.
- **chat.Store** (`chat.Store`, default impl `chat/sqlite.Store`): SQLite-backed
  persistence for `chat_sessions` and `chat_messages`. Versioned migrations via
  the `schema_migrations` table; WAL mode with `MaxOpenConns=5` so concurrent
  readers (`ListMessages`, `MaxSeq`, `GetSession`) bypass the single-writer gate
  that `chat.Manager.mu` enforces above the pool. Unique index on
  `(session_id, seq)` is the safety net behind the in-memory seq cache.
- **chat.SSEHub** (`chat.SSEHub`): per-session SSE fan-out. Each `sessionHub`
  has a 128-entry ring buffer of recent events and a subscriber set; replays the
  ring on `Subscribe(sinceSeq)` so reconnects within the ring window are
  gapless. `Manager.DeleteSession` calls `Hub.Drop(sessionID)` to release the
  per-session hub so memory does not grow with session churn. Two event kinds
  share the hub: `message` (a new transcript row, with seq + role + content) and
  `session_updated` (a metadata change — `context_tokens`, `rehydration_active`,
  model, and `status` for lifecycle transitions — with no transcript content). The
  `status` field uses a pointer so `omitempty` distinguishes "no lifecycle change"
  from a deliberate transition. All active-transition entry points emit it: `OpenSession`
  (cold→active and warm-idle→active), `OnSubscribe` callback (warm-idle→active),
  `MarkWarmIdle` (active→warm-idle), and `EndSession` (any→cold, always paired with
  `RehydrationActive: false`). Publishes run in a goroutine so callers don't block on the
  hub mutex. The browser's `useChatStream` hook routes `session_updated` events into the
  header state; when the `status` field changes, it dispatches `notifyChatSessionsChanged`
  via `queueMicrotask` (so StrictMode double-invokes don't double-dispatch) — `useChatSessions`
  debounces that event with a 100 ms window to coalesce fan-out from multiple open panes into
  a single `/api/chats` refetch that updates the sidebar status dot.
- **chat.IdleReaper** (`chat.IdleReaper`): scans `warm-idle` sessions older than
  `IdleTTL` and ends them. `Stop()` is `sync.Once`-guarded so repeated shutdown
  calls don't panic.
- **API handlers** (`api/*`): thin HTTP layer. Deserialize → call CardService →
  serialize. No business logic, no direct store/git/lock access.
  `GET /api/runner/logs` has two modes: card-scoped (uses the session manager
  for replay-on-reconnect) and project-scoped legacy proxy (forwards the raw
  runner SSE stream verbatim).
- **MCP server** (`mcp/*`): exposes tools (card operations) and prompts (skill
  files) via Streamable HTTP on `/mcp` (registered for `POST`, `GET`, and
  `DELETE`). Registered on the same `http.ServeMux` as the REST API, so it
  inherits the shared middleware chain (recovery, security headers, CORS,
  requestID, observe, bodyLimit, csrfGuard) with no special wrapping — the
  body-limit (5 MB) is applied uniformly across all routes.
- **Context-aware logger** (`ctxlog`): stores a `*slog.Logger` enriched with a
  `request_id` attribute in the request context. The `requestID` middleware in
  `internal/api/` calls `ctxlog.WithRequestID(ctx, id)` on every incoming
  request. All log sites in `internal/api/`, `internal/service/`,
  `internal/storage/`, and `internal/runner/` retrieve the logger via
  `ctxlog.Logger(ctx)` so every log line emitted during a request carries the
  same correlation ID. Falls back to `slog.Default()` for background contexts
  that bypass the middleware (e.g. stall scanner goroutine). Also stores a
  `*MCPCall` in the context (via `ctxlog.WithMCPCall`) for `/mcp` requests;
  `mcpRequestInfoMiddleware` in `internal/mcp/server.go` populates it with the
  JSON-RPC `method` and tool `name`, which the `observe` middleware then appends
  as `mcp_method` / `mcp_tool` fields on the per-request log line.
- **Clock** (`clock`): tiny `clock.Clock` interface with `Real()` and a fake
  implementation used by tests. `lock.Manager`, `CardService`, `chat.Manager`,
  and `refresh.Registry` all read time through this interface so a single fake
  drives every time-sensitive subsystem deterministically. The service layer
  adopts the lock manager's clock so stall detection and the timeout-checker
  ticker share one monotonic reading — wiring two different clocks across these
  subsystems is a latent test-flake source.
- **Refresh Registry** (`refresh.Registry`): in-memory tracker for in-flight
  knowledge-base refresh jobs, keyed by `(project, repo)`. Holds the
  acquire/running/terminal lifecycle plus progress fields populated by the MCP
  `update_refresh_progress` tool. State is process-local; a restart loses
  tracking but in-flight runner containers continue to call back through MCP.
  `service.WriteKnowledgeDocs` calls `MarkCommitted` after a successful
  Refresh-source commit; the registry's `Janitor` (started from `main.go`)
  sweeps terminal entries on an interval.
- **Event Bus** (`events.Bus`): in-process publish/subscribe. The bus has a drop
  counter (`contextmatrix_event_bus_drops_total`) — subscribers that fall behind
  the per-subscriber channel cap drop events rather than blocking the publisher.
- **gitsync Syncer** (`gitsync.Syncer`): background loop that pulls the boards
  remote (when `boards.auto_pull` is enabled) and pushes after each successful
  commit (when `boards.auto_push` is enabled). Coordinates with the service
  layer through `LockWrites`/`UnlockWrites` and with the commit queue through
  `Pause`/`Resume`/`AwaitIdle` so rebases never race against in-flight go-git
  commits.
- **GitHub integration** (`github`): three pieces — `client.go` (HTTP client for
  GitHub REST API used during issue import / branch listing), `parse.go` (issue
  → card mapping rules), `syncer.go` (per-project import loop driven by
  `github.import_issues`). Auth is delegated to the shared
  `githubauth.TokenGenerator` provider; the package never reads tokens directly.
- **Config** (`config`): typed YAML loader. Every field has a documented
  `CONTEXTMATRIX_*` env override; `config.yaml.example` is the canonical
  reference.
- **Metrics** (`metrics`): declares all Prometheus metric vars and exposes a
  `Register(prometheus.Registerer)` function called once at startup in
  `main.go`. Metrics are served at `GET /metrics` on the **admin listener** only
  (`admin_port`, bound to `admin_bind_addr`; default loopback). The main
  listener does not expose `/metrics`. The `observe` middleware in
  `internal/api/` wraps every REST route to record per-route HTTP RED
  (rate/error/duration) metrics; unmatched routes collapse to a single
  `path="unmatched"` label to bound cardinality. SSE endpoints are excluded from
  the latency histogram because their connection lifetime would drown out real
  REST signal. Additional instrumentation: SSE gauge in `internal/api/events.go`
  and `runner_logs.go`, event-bus drop counter in `internal/events/`, git-sync
  histogram in `internal/gitops/`, stall-scanner histogram and counter in
  `internal/service/`, unknown-model counter
  (`contextmatrix_report_usage_unknown_model_total`, labeled by model) in
  `internal/service/service_usage.go` (incremented when `report_usage` is called
  with a model absent from `token_costs` — alert on a sustained non-zero rate to
  detect misconfigured or newly deployed models). See the full metric list in
  `internal/metrics/metrics.go`.

## Git repository scope

The boards directory is a separate git repository from the source code. The
`gitops.Manager` operates on `cfg.Boards.Dir`, not the source tree. File paths
passed to `CommitFile()` / `CommitFiles()` are relative to that directory (e.g.,
`project-alpha/tasks/ALPHA-001.md`).

```text
~/code/contextmatrix/           # source code repo
  cmd/, internal/, web/, workflow-skills/
  config.yaml                   # boards.dir: ~/boards/contextmatrix

~/boards/contextmatrix/         # boards repo (separate git repo)
  project-alpha/
    .board.yaml
    tasks/
    templates/
```

If the boards directory does not exist or is not a git repo on startup, the
server creates it and runs `git init`.

`boards.dir` in `config.yaml` should point outside the source tree — an absolute
path or a path like `~/boards/contextmatrix`, not `./boards`.

## File layout

**Source code:**

```text
cmd/contextmatrix/main.go
internal/
  board/             # domain types + Validator + state machine
  storage/           # FilesystemStore + Store interface
  gitops/            # gitops.Manager + CommitQueue (per-project workers)
  lock/              # claim/release/heartbeat + stall scan
  service/           # CardService orchestration (split across service_*.go)
  api/               # REST handlers + SSE + middleware chain + CSRF gate
  mcp/               # MCP server (Streamable HTTP /mcp) + mcpcontext/
  runner/            # webhook client + HMAC + reconciler
    sessionlog/      # per-card SSE buffer + fan-out hub
  chat/              # chat.Manager + Store + SSEHub + IdleReaper + runner bridge
    sqlite/          # SQLite-backed chat persistence + versioned migrations
    transcript/      # pure transcript-shaping for cold-reopen resume payloads
  refresh/           # in-flight knowledge-base refresh registry + janitor
  github/            # GitHub client + issue parser + import syncer
  gitsync/           # boards repo background pull/push syncer
  events/            # in-process pub/sub (events.Bus)
  config/            # typed YAML loader
  ctxlog/            # request_id context logger + MCPCall context
  metrics/           # Prometheus metric vars + Register()
  clock/             # injectable clock (Real + fakes for tests)
web/                 # React + Vite frontend (embedded via web/embed.go)
workflow-skills/     # skill markdown files served via MCP prompts
go.mod
config.yaml.example
Makefile
```

**Boards repo:**

```text
project-alpha/
  .board.yaml
  templates/
    task.md
    bug.md
    feature.md
  tasks/
    ALPHA-001.md
    ALPHA-002.md
project-beta/
  .board.yaml
  templates/
  tasks/
```
