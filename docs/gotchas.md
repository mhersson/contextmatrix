# Gotchas

- **YAML frontmatter parsing:** use `bytes.SplitN(content, []byte("---"), 3)` to
  split. Element 0 is empty (before first `---`), element 1 is YAML, element 2
  is body. Handle `\r\n` line endings.
- **`gitops.CommitQueue` per-project ordering, idle teardown:** the queue spawns
  one goroutine per `Project` value and serializes that project's commits in
  enqueue order; different projects commit in parallel. Production wires
  `gitops.NewCommitQueue(git, 0, gitops.WithIdleTimeout(30*time.Minute))` in
  `main.go`, so a worker that goes idle for 30 minutes exits and the next
  `Enqueue` for that project spawns a fresh one — `projectWorker.closed` plus
  the per-worker mutex stop an Enqueue from sending into a channel the worker is
  about to abandon. Do not assume worker identity across an idle gap when
  reasoning about cached state.
- **`CardService.LockWrites` is paired with queue pause + drain:** the gitsync
  layer holds `LockWrites` across a pull+rebase so no card mutation interleaves
  with the rebase. The same function also calls `commitQueue.Pause()` and
  `AwaitIdle(ctx)` with a 30-second budget so an in-flight go-git commit cannot
  collide with the shell-git rebase on `.git/index.lock`. `UnlockWrites` must
  call `Resume()` before releasing `writeMu`; reversing the order leaves the
  queue paused under a fresh write.
- **Deferred git commits (`boards.git_deferred_commit`):** When
  `boards.git_deferred_commit: true` in `config.yaml`, agent mutations
  (heartbeats, log entries, intermediate updates) are batched and committed in a
  single flush at release/complete time instead of per-operation. This reduces
  git churn during long agent work sessions. However, two categories of mutation
  **always commit immediately**, even when deferred mode is on: (1) card
  creation — both the card file and `.board.yaml` are committed together so the
  new card survives a `git pull` on another machine; (2) human edits to
  unclaimed cards via the REST API — the PUT/PATCH handlers set
  `ImmediateCommit: true` when `card.AssignedAgent == ""`, triggering an
  immediate commit. MCP tool callers (agents) never set this flag, so their
  commits continue to defer normally.
- **MCP middleware chain and body limit:** `/mcp` is registered on the same
  inner `http.ServeMux` as the REST API, so it automatically inherits the shared
  middleware chain (recovery, security headers, CORS when enabled, request ID,
  observe/metrics+logging, body limit, csrfGuard). The body-size cap is **5 MB**
  (`maxRequestBodySize`) — sized to the largest legitimate MCP card payload and
  applied to every route without a per-route override (`POST /api/images` raises
  it to 11 MB via `bodyLimitOverrides`). Requests with a `Content-Length`
  exceeding 5 MB are rejected with `413 Payload Too Large` before the body is
  read; requests
  without `Content-Length` are capped during reads via `http.MaxBytesReader`.
- **SSE and MCP streaming vs. `WriteTimeout`:** Go's `http.Server.WriteTimeout`
  is an absolute deadline measured from when request headers are read — it is
  NOT reset by intermediate writes (keepalive comments, partial event data,
  etc.). Long-lived SSE connections will always hit it, causing the client to
  see an abrupt disconnect every `WriteTimeout` seconds regardless of keepalive
  activity. The fix is
  `http.NewResponseController(w).SetWriteDeadline(time.Time{})` called before
  entering the streaming loop. This clears the deadline for that one connection
  only; all other endpoints keep the server-wide timeout. Applied in
  `internal/api/events.go` (SSE event stream), `internal/api/runner_logs.go`
  (runner SSE log stream), and as the `clearWriteDeadlineForStreaming`
  middleware in `internal/mcp/server.go` (MCP GET stream). The MCP middleware
  scopes the clear to `GET` requests only — `POST` and `DELETE` (short RPC
  calls) retain the normal `WriteTimeout`. **Critical:** `ResponseController`
  finds the underlying connection by calling `Unwrap()` on the `ResponseWriter`.
  Any middleware that wraps the writer (e.g., the logging middleware's
  `responseWriter`) must implement `Unwrap() http.ResponseWriter` or
  `SetWriteDeadline` silently fails — the error is non-fatal, so the handler
  continues but the timeout stays active.
- **Tailwind v4 preflight strips `list-style` from `ul`/`ol`:** `@import "tailwindcss"` injects `@layer base { ol, ul { list-style: none } }`, which overrides browser UA defaults. Third-party markdown libraries (e.g. `@uiw/react-markdown-preview`) set `list-style-type` only on nested levels and rely on UA defaults for the top level — so bullets and numbers silently disappear. Restore them with explicit `!important` rules scoped to the library's wrapper class (e.g. `.wmde-markdown ul { list-style: disc !important }`). Also re-assert the nested cascade (`lower-roman`, `lower-alpha`) because your `!important` on the base rule wins over the library's non-`!important` nested rules.
- **Frontend embed:** `//go:embed all:dist` in `web/embed.go` (package `web`).
  The `all:` prefix is required so dotfiles under `dist/` are included; a plain
  `web/dist/*` glob would silently miss them. Must build frontend _before_
  building Go binary. SPA routing requires a fallback to `index.html` for all
  non-API, non-file routes.
- **404 handling is React Router's job:** `newSPAHandler` returns `index.html`
  for every path that isn't an `/api/` prefix, `/healthz`, `/readyz`, `/mcp`, or
  a real static file. The Go layer never returns a 404 for UI paths. Unknown
  routes are caught by `<Route path="*" element={<NotFound />} />` placed as the
  last route in both `App.tsx` (top-level) and `ProjectShell.tsx` (nested
  project routes). If you add a new `Routes` subtree, add its own catch-all or
  users will see a blank screen instead of the 404 page.
- **stdlib URL params:** use `r.PathValue("project")` (Go 1.22+). Route patterns
  use `{project}` syntax:
  `mux.HandleFunc("GET /api/projects/{project}", handler)`.
- **`time.Duration` in YAML:** `time.Duration` doesn't unmarshal from strings
  like `"30m"` with `gopkg.in/yaml.v3`. Either use a custom type with
  `UnmarshalYAML`, or store as string in config and parse with
  `time.ParseDuration()` at load time.
- **`/healthz` and `/readyz` requests are not logged:** the HTTP logging
  middleware skips `slog.Info` for `GET /healthz` and `GET /readyz` to prevent
  k8s liveness/readiness probe traffic from spamming logs. Both endpoints still
  respond normally — only the log line is suppressed. If you expect to see probe
  traffic in logs for debugging, hit any other path or check the endpoints
  directly with `curl`.
- **Firefox per-origin SSE connection limit:** Firefox's connection manager
  cancels in-flight requests to the same origin with `NS_BINDING_ABORTED` /
  "connection interrupted while the page was loading" when a new navigation-
  adjacent fetch pushes the total past its limit. Practically: if the app opens
  ≥ 3 `EventSource('/api/events')` connections and then a 4th SSE stream opens
  at the same origin (e.g. `/api/runner/logs` on HITL start), Firefox aborts the
  earlier three simultaneously. Chrome does not exhibit this behaviour. The fix
  is to share a single `EventSource` for the whole app via `SSEProvider` and fan
  events out to subscribers in-process — see `web/src/hooks/useSSEBus.tsx`.
  Never open more than one `EventSource` per distinct URL; use the subscriber
  API for additional consumers of the same stream. For runner logs specifically,
  `ProjectShell` owns a single card-scoped `useRunnerLogs` call (enabled only
  while the selected card is a HITL running session) and passes the resulting
  `LogEntry[]` array down to `CardChat` as a prop — `CardChat` does not open its
  own `EventSource`.
- **`sessionlog.Manager` fan-out invariants:** `readUpstream` (card-scoped) and
  `readProjectUpstream` (project-scoped) both append to the ring buffer and fan
  out to subscribers under a single `m.mu` lock. These two operations must stay
  under the same lock — separating them reintroduces the duplicate-delivery race
  where an event lands in the snapshot AND in `sub.pending` for the same
  subscriber. The primed-flag protocol (`sub.primed`, `sub.pending`) is what
  enforces snapshot-before-live ordering: the pump stages live events in
  `sub.pending` while `sub.primed` is false; the snapshot goroutine in
  `Subscribe`/`SubscribeProject` flips `primed = true` (under `m.mu`) only after
  draining both the snapshot slice and `sub.pending` into the subscriber's
  channel. Do not bypass this gate. Two additional channels on `subscriber`
  enforce lifecycle safety: `done` (closed by `unsub` or `Stop`/terminal-error)
  signals the snapshot goroutine to exit early; `snapDone` (closed by the
  snapshot goroutine via `defer`) signals that it has exited. `Stop` and the
  terminal-error path in both pumps call `closeSubscriber`, which closes `done`,
  waits on `snapDone` (up to 1 s), then sends the terminal event and calls
  `close(ch)`. This ordering is mandatory: closing `ch` while the snapshot
  goroutine is still sending on it panics. `close(done)` is guarded by
  `sync.Once` (`doneOnce`) so both `unsub` and `Stop` can call it safely. The
  snapshot goroutine blocks on each channel send
  (`select { case ch <- evt: case <-sub.done: return }`) rather than dropping —
  slow subscribers receive the full snapshot; they are never silently truncated.
  Project-scoped sessions use the key `"project:<name>"` in the shared
  `activeSessions`, `pendingSubs`, and `sessions` maps; this prefix prevents
  collisions with card IDs. The only difference from the card-scoped pump is
  that `readProjectUpstream` does not filter by card ID — it accepts every event
  and preserves the originating `CardID` field on `sessionlog.Event`.
- **`request_id` log correlation:** every HTTP request gets a `request_id` UUID
  injected into its context by the `requestID` middleware via
  `ctxlog.WithRequestID(ctx, id)`. All log sites must use `ctxlog.Logger(ctx)` —
  not `slog.Default()` or a package-level logger — otherwise the log line will
  not carry the correlation ID. Background goroutines (stall scanner, git-pull
  ticker) do not go through the middleware; `ctxlog.Logger(ctx)` falls back to
  `slog.Default()` safely in those paths.
- **MCP tool name in the request log line:** for `POST /mcp` requests the
  `observe` middleware emits two extra fields alongside the standard `method`,
  `path`, `status`, `duration_ms`, and `request_id` fields: `mcp_method`
  (JSON-RPC method, e.g. `tools/call`) and `mcp_tool` (tool name, e.g.
  `claim_card` or `report_usage`). Both fields are omitted for non-MCP routes
  and for MCP methods other than `tools/call` (e.g. `initialize`) where there is
  no tool name. The extraction is best-effort — a body-peeking middleware
  (`mcpRequestInfoMiddleware` in `internal/mcp/server.go`) reads the request
  body, parses the JSON-RPC envelope, restores the body, and writes the results
  into a `*ctxlog.MCPCall` stashed in the context by `observe`. Errors during
  extraction are swallowed; the log line is still emitted with whatever fields
  were successfully extracted.
- **`/metrics` and pprof live on the admin port:** Prometheus scraping
  (`GET /metrics`) and `/debug/pprof/*` are served only on the admin listener
  (`admin_port`), which defaults to `127.0.0.1` (`admin_bind_addr`). The main
  listener never exposes them. There is no authentication on the admin listener
  — keep it loopback-only, or gate with firewall / NetworkPolicy / service-mesh
  rules if your scrape setup requires a non-loopback bind. A non-loopback bind
  logs a warning at startup.
- **PAT mode requires specific permissions:** when `github.auth_mode: pat`, the
  fine-grained PAT must have `Contents: Read and write` on the boards repo
  **and** `Issues: Read-only` on each project repo referenced in `.board.yaml`
  that has `github.import_issues: true`. PAT mode only works with GitHub
  (github.com or GHEC/GHES); for non-GitHub hosts, use a different auth mode.
- **All git remote URLs must be HTTPS:** `boards.git_remote_url` and
  `task_skills.git_remote_url` are validated at startup and must start with
  `https://` regardless of `github.auth_mode`. SSH URLs are rejected
  unconditionally — there is no SSH transport fallback.
- **Chat SQLite: WAL + MaxOpenConns + manager-level writer mutex:** the chat
  store sets `MaxOpenConns=5` so concurrent readers (`ListMessages`, `MaxSeq`,
  `GetSession`) do not queue behind a writer. SQLite remains a single-writer
  engine regardless of pool size; the single-writer gate is `chat.Manager.mu`,
  held across the entire seq-assignment + store insert in `AppendMessage`. Do
  not move the store write outside the lock — disk insertion order must match
  seq order, and the in-memory seq cache must stay consistent with the on-disk
  `(session_id, seq)` UNIQUE index. The pool size is a reader-concurrency knob;
  raising the writer concurrency requires changing the manager's locking model,
  not the pool.
- **SQLite driver is `modernc.org/sqlite`, pragmas live in the DSN:** the chat
  store opens
  `sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")`.
  The driver name is `"sqlite"` (not `"sqlite3"` — that registration belongs to
  `mattn/go-sqlite3`, which we do **not** import to keep the binary CGO-free).
  The `_pragma=...` query-string parameters are a `modernc.org` extension;
  switching drivers means rewriting these as `PRAGMA` statements executed on the
  open connection.
- **Chat SSE per-session subscriber cap:** `SSEHub.Subscribe` returns an error
  if a session already has 32 live subscribers (`maxSubscribersPerSession` in
  `internal/chat/sse.go`). A normal browser tab is one subscriber; the cap
  blocks runaway clients from exhausting goroutines and channel memory. The
  128-entry ring buffer per session is also a hard cap — events older than the
  ring window are gone, and reconnects past the window depend on the REST
  bootstrap in `useChatStream` to backfill. Session-update events
  (`session_updated`) are NOT stored in the ring; only `message` events are,
  since session metadata is meant to be re-fetched on reconnect.
- **Op-store schema is a clean-cut create — existing `chats.db` must be deleted
  on upgrade:** `ensureSchema` in `internal/opstore/sqlite/schema.go` runs plain
  `CREATE TABLE IF NOT EXISTS` DDL for every table (`model_blacklist`,
  `chat_sessions`, `chat_messages`, `chat_cost_archive`) in the shared `ops.db`.
  There is **no migration ledger** — no `schema_migrations(version, applied_at)`
  table, no stepwise history, no `addColumnIfMissing` helper. The DB is not
  migrated: an obsolete one is deleted and recreated by the operator. To change
  the schema, edit the `ensureSchema` DDL directly — it is all idempotent
  `CREATE ... IF NOT EXISTS`. A `chats.db` from a previous install is not
  forward-compatible; operators must delete it before starting the server.
- **`useChatStream` ring buffer + REST bootstrap seam:** the hook uses
  `useRingBuffer(2000)` and pairs the SSE subscription with a REST bootstrap via
  `GET /api/chats/{id}/messages?since_seq=0`. On mount / sessionID change, the
  hook fetches the persisted transcript first, records the highest `seq`, then
  subscribes to the SSE stream with `since_seq=<last>`. SSE events whose seq
  falls inside the bootstrap window are deduped on the client. Reverting to
  SSE-only (no bootstrap) loses everything older than the server-side 128-entry
  ring on refresh.
- **Chat rehydration is best-effort and never blocks `/open`:** the runner's
  `prepareChatResume` writes `resume.jsonl` + `resume.meta.json` into a
  per-container host directory before starting the container. If the write fails
  (host tmp not writable, disk full, etc.), `manager.go` logs
  `StartChat: rehydration file prep failed; starting fresh agent`, omits
  `CM_CHAT_RESUME=1`, and starts the container anyway. The stdin priming
  envelope is still written (it is gated on the CM payload's `resume`, not on
  the file-write outcome), so the agent receives the instructions, fails to read
  `/run/cm-chat/resume.jsonl`, and calls `chat_rehydration_complete` with a
  summary that says so. `/open` always returns `200`; surface failures via the
  transcript, never by refusing to open.
- **`rehydration_phase` stamping prevents reopen pollution:** every message
  appended while `Session.RehydrationActive=TRUE` gets stamped with
  `rehydration_phase=TRUE` in `chat_messages`. `chat.transcript.Build` drops
  those rows when assembling the next resume payload, so the resumed agent never
  sees prior agents' `ls`/`Read`/`Bash` chatter — only real conversation turns
  plus the prior `chat_rehydration_complete` summaries. Without this filter,
  each reopen would compound the previous reopen's rehydration noise into the
  next transcript.
- **`claude -p PROMPT --input-format stream-json` does NOT auto-execute
  `PROMPT`:** Claude treats `-p` as system context when stream-json input is
  enabled, not as a user message. The rehydration priming therefore has to
  arrive as a stream-json `user`-typed envelope written to stdin _after_
  `AttachChatStdin` succeeds — see `webhook/chat.go` and
  `streammsg.BuildUserMessage`. The same applies to any "kick the agent off
  with X" pattern in chat or HITL modes: use a stream-json user envelope on
  stdin, not `-p`.
