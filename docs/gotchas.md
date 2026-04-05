# Gotchas

- **YAML frontmatter parsing:** use `bytes.SplitN(content, []byte("---"), 3)` to
  split. Element 0 is empty (before first `---`), element 1 is YAML, element 2
  is body. Handle `\r\n` line endings.
- **`next_id` atomicity:** since the server is single-process, a mutex on
  `ProjectConfig` is sufficient. Multi-instance would need file locking.
- **`go-git` performance:** fine for ContextMatrix boards (small files, <10k
  cards). If it becomes an issue, shell out to `git` binary.
- **Deferred git commits (`git_deferred_commit`):** When
  `git_deferred_commit: true` in `config.yaml`, agent mutations (heartbeats, log
  entries, intermediate updates) are batched and committed in a single flush at
  release/complete time instead of per-operation. This reduces git churn during
  long agent work sessions. However, two categories of mutation **always commit
  immediately**, even when deferred mode is on: (1) card creation — both the
  card file and `.board.yaml` are committed together so the new card survives a
  `git pull` on another machine; (2) human edits to unclaimed cards via the REST
  API — the PUT/PATCH handlers set `ImmediateCommit: true` when
  `card.AssignedAgent == ""`, triggering an immediate commit. MCP tool callers
  (agents) never set this flag, so their commits continue to defer normally.
- **SSE headers:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `Connection: keep-alive`. Must call `Flusher.Flush()` after each event.
- **SSE and MCP streaming vs. `WriteTimeout`:** Go's `http.Server.WriteTimeout`
  is an absolute deadline measured from when request headers are read — it is
  NOT reset by intermediate writes (keepalive comments, partial event data, etc.).
  Long-lived SSE connections will always hit it, causing the client to see an
  abrupt disconnect every `WriteTimeout` seconds regardless of keepalive activity.
  The fix is `http.NewResponseController(w).SetWriteDeadline(time.Time{})` called
  before entering the streaming loop. This clears the deadline for that one
  connection only; all other endpoints keep the server-wide timeout.
  Applied in `internal/api/events.go` (SSE event stream) and as the
  `clearWriteDeadlineForStreaming` middleware in `internal/mcp/server.go` (MCP
  GET stream). The MCP middleware scopes the clear to `GET` requests only —
  `POST` and `DELETE` (short RPC calls) retain the normal `WriteTimeout`.
  **Critical:** `ResponseController` finds the underlying connection by calling
  `Unwrap()` on the `ResponseWriter`. Any middleware that wraps the writer (e.g.,
  the logging middleware's `responseWriter`) must implement
  `Unwrap() http.ResponseWriter` or `SetWriteDeadline` silently fails — the
  error is non-fatal, so the handler continues but the timeout stays active.
- **Frontend embed:** `//go:embed web/dist/*` in `main.go`. Must build frontend
  _before_ building Go binary. SPA routing requires a fallback to `index.html`
  for all non-API, non-file routes.
- **404 handling is React Router's job:** `newSPAHandler` returns `index.html`
  for every path that isn't an `/api/` prefix, `/healthz`, `/mcp`, or a real
  static file. The Go layer never returns a 404 for UI paths. Unknown routes are
  caught by `<Route path="*" element={<NotFound />} />` placed as the last route
  in both `App.tsx` (top-level) and `ProjectShell.tsx` (nested project routes).
  If you add a new `Routes` subtree, add its own catch-all or users will see a
  blank screen instead of the 404 page.
- **Tailwind purge:** `content` in `tailwind.config.js` must include
  `./src/**/*.tsx` or classes get stripped.
- **Activity log bloat:** capped at 50 entries in frontmatter. Older entries are
  only in git history. If an agent writes very frequently, entries may be lost
  between git commits — this is acceptable.
- **Vite proxy:** `vite.config.ts` must proxy `/api` to `http://localhost:8080`
  during dev. Without this, frontend can't reach the backend.
- **stdlib URL params:** use `r.PathValue("project")` (Go 1.22+). Route patterns
  use `{project}` syntax:
  `mux.HandleFunc("GET /api/projects/{project}", handler)`.
- **`time.Duration` in YAML:** `time.Duration` doesn't unmarshal from strings
  like `"30m"` with `gopkg.in/yaml.v3`. Either use a custom type with
  `UnmarshalYAML`, or store as string in config and parse with
  `time.ParseDuration()` at load time.
- **MCP Streamable HTTP transport:** `POST /mcp` handles all MCP traffic.
  Responses are either plain JSON (for non-streaming operations) or
  `text/event-stream` (for streaming tool results). Registered on the same
  `http.ServeMux` as the REST API — no separate server or port needed.
- **MCP auth:** support an optional bearer token (`mcp.auth_token` in
  `config.yaml`). When set, the `/mcp` endpoint requires
  `Authorization: Bearer <token>`. Essential for container deployments exposed
  beyond localhost.
- **MCP prompts + card context:** most prompt handlers return delegation
  wrappers (not raw skill content); interview skills (`create-task`,
  `init-project`) return raw content for inline execution. The `get_skill` tool
  fetches card context at execution time, calling the service layer in-process.
  The MCP handler and HTTP API share the same `CardService` instance. The
  `## Agent Configuration` section is stripped from all skill content delivered
  via `get_skill` and prompt handlers — the required model is returned as a
  separate `model` field.
- **Sub-agent death during idle user-approval wait:** Claude Code can kill a
  sub-agent between turns if the conversation goes quiet (e.g., while waiting
  for the user to read and approve a plan). The fix is to never have a sub-agent
  wait for user input — instead, the sub-agent should write its output to the
  card body and return immediately, and let the always-alive main agent (CC)
  handle the user interaction. All skills that previously had this problem have
  been fixed:
  - `create-plan`: Phase 1 drafts and writes the plan then returns
    `PLAN_DRAFTED`; CC presents the plan to the user and collects approval;
    Phase 2 creates subtasks from the already-approved plan.
  - `review-task`: The review sub-agent writes `## Review Findings` to the card
    body and returns `REVIEW_FINDINGS` immediately; CC presents findings to the
    user and collects the approve/reject decision directly.
  - `document-task`: The doc sub-agent writes files to disk and returns
    `DOCS_WRITTEN` immediately — no user approval gate before writing, since
    docs are built from already-reviewed code; CC presents the summary after.
  - `create-task` and `init-project`: These interview skills now run inline in
    CC (no sub-agent at all) — see the "Interview skills run inline" entry
    below. Any new skill that must get user approval before continuing should
    follow the same split-phase pattern: sub-agent writes output to card body
    and returns a structured result immediately; CC handles the user
    interaction.
- **Interview skills (create-task, init-project) run inline:** These skills
  require multi-turn back-and-forth conversations with the user (gathering
  requirements, confirming config). Delegating them to a sub-agent breaks this
  because the `Agent` tool does not support relaying multiple user turns back
  into the sub-agent. Their prompt handlers return the raw skill content (with
  `## Agent Configuration` stripped) rather than a delegation wrapper, so the
  main agent executes them directly in its own context. **Never delegate
  `create-task` or `init-project` to a sub-agent.**
- **Execute-task agents never spawn review:** The `execute-task` skill
  explicitly instructs agents to ignore any `next_step` field returned by
  `complete_task` (e.g., when the parent card transitions to `review`). The
  lifecycle continuation (spawning review sub-agents) was removed from
  execute-task agents because it caused nested agent chains with unpredictable
  lifetimes. The orchestrator (main CC) is solely responsible for detecting that
  the parent entered `review` and spawning the review sub-agent.
- **`/healthz` requests are not logged:** the HTTP logging middleware skips
  `slog.Info` for `GET /healthz` to prevent k8s liveness/readiness probe traffic
  from spamming logs. The endpoint still responds normally — only the log line
  is suppressed. If you expect to see probe traffic in logs for debugging, hit
  any other path or check the endpoint directly with `curl`.
- **Health-check polling interval:** the monitoring loop in `create-plan.md`
  polls every 1 minute (not 2-3 min). Shorter intervals mean stalled agents are
  detected and respawned faster, reducing idle time for the user.
- **Agents must never use curl:** `CLAUDE.md` mentions `curl` for "Manual
  verification for API tasks" — that applies to human developers checking API
  handler code, NOT to agents interacting with the board. Agents must use MCP
  tools exclusively (`claim_card`, `heartbeat`, `update_card`, `complete_task`,
  etc.). Using curl bypasses claim tracking, heartbeats, and the event bus,
  leaving cards orphaned. This rule is enforced in the `workflowPreamble`
  prepended to every skill prompt.
