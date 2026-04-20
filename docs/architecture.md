# Architecture

## Data flow

Every card mutation follows the same pipeline through the service layer:

```text
API handler (deserialize, validate)
  → CardService
    → StateMachine.ValidateCard() — type, state, priority checks
    → Store.CreateCard()          — write .md file, update in-memory index
    → GitManager.CommitFiles()    — git add + commit
    → EventBus.Publish()          — notify SSE subscribers
  ← return card
← serialize response
```

The MCP server follows the same path — it calls `CardService` methods, never the
store or git layer directly.

## Component responsibilities

- **Store** (`storage.FilesystemStore`): reads/writes `.md` files and
  `.board.yaml` to disk. Maintains an in-memory index. No knowledge of git,
  events, or locking.
- **GitManager** (`gitops.Manager`): stages and commits files, handles push/pull
  with remote repositories. No knowledge of cards or events.
- **Lock Manager** (`lock.Manager`): enforces claim/release/heartbeat rules.
  Reads cards via the store to check ownership but does not write — it returns
  modified card data to the caller (the service layer).
- **Event Bus** (`events.Bus`): in-process pub/sub. Receives events, fans out to
  subscribers.
- **StateMachine** (`board.StateMachine`): validates transitions and card
  fields. Pure functions, no side effects.
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
- **API handlers** (`api/*`): thin HTTP layer. Deserialize → call CardService →
  serialize. No business logic, no direct store/git/lock access.
  `GET /api/runner/logs` has two modes: card-scoped (uses the session manager
  for replay-on-reconnect) and project-scoped legacy proxy (forwards the raw
  runner SSE stream verbatim).
- **MCP server** (`mcp/*`): exposes tools (card operations) and prompts (skill
  files) via Streamable HTTP on `POST /mcp`. Registered on the same
  `http.ServeMux` as the REST API, so it inherits the shared middleware chain
  (recovery, security headers, CORS, requestID, observe, bodyLimit) with no
  special wrapping — the body-limit (5 MB) is applied uniformly across all
  routes.
- **Context-aware logger** (`ctxlog`): stores a `*slog.Logger` enriched with a
  `request_id` attribute in the request context. The `requestID` middleware in
  `internal/api/` calls `ctxlog.WithRequestID(ctx, id)` on every incoming
  request. All log sites in `internal/api/`, `internal/service/`,
  `internal/storage/`, and `internal/runner/` retrieve the logger via
  `ctxlog.Logger(ctx)` so every log line emitted during a request carries the
  same correlation ID. Falls back to `slog.Default()` for background contexts
  that bypass the middleware (e.g. stall scanner goroutine).
- **Metrics** (`metrics`): declares all Prometheus metric vars and exposes a
  `Register(prometheus.Registerer)` function called once at startup in
  `main.go`. Metrics are served at `GET /metrics` on the **admin listener**
  only (`admin_port`, bound to `admin_bind_addr`; default loopback). The main
  listener does not expose `/metrics`. The `observe` middleware in
  `internal/api/` wraps every REST route to record per-route HTTP RED
  (rate/error/duration) metrics; unmatched routes collapse to a single
  `path="unmatched"` label to bound cardinality. SSE endpoints are excluded
  from the latency histogram because their connection lifetime would drown
  out real REST signal. Additional instrumentation: SSE gauge in
  `internal/api/events.go` and `runner_logs.go`, event-bus drop counter in
  `internal/events/`, git-sync histogram in `internal/gitops/`, stall-scanner
  histogram and counter in `internal/service/`. See the full metric list in
  `internal/metrics/metrics.go`.

## Git repository scope

The boards directory is a separate git repository from the source code. The
`GitManager` operates on `cfg.BoardsDir`, not the source tree. File paths passed
to `CommitFiles()` are relative to that directory (e.g.,
`project-alpha/tasks/ALPHA-001.md`).

```text
~/code/contextmatrix/           # source code repo
  cmd/, internal/, web/, skills/
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
  board/         # domain types
  storage/       # FilesystemStore
  gitops/        # GitManager
  lock/          # claim/release/heartbeat
  service/       # CardService orchestration
  api/           # REST handlers + SSE
  mcp/           # MCP server
  runner/        # webhook client
  events/        # in-process pub/sub
  config/        # config loading
  ctxlog/        # request_id context logger
  metrics/       # Prometheus metric vars + Register()
web/
skills/
  create-task.md
  create-plan.md
  execute-task.md
  review-task.md
  document-task.md
  init-project.md
  run-autonomous.md
go.mod
config.yaml
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
