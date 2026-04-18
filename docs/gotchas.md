# Gotchas

- **YAML frontmatter parsing:** use `bytes.SplitN(content, []byte("---"), 3)` to
  split. Element 0 is empty (before first `---`), element 1 is YAML, element 2
  is body. Handle `\r\n` line endings.
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
- **stdlib URL params:** use `r.PathValue("project")` (Go 1.22+). Route patterns
  use `{project}` syntax:
  `mux.HandleFunc("GET /api/projects/{project}", handler)`.
- **`time.Duration` in YAML:** `time.Duration` doesn't unmarshal from strings
  like `"30m"` with `gopkg.in/yaml.v3`. Either use a custom type with
  `UnmarshalYAML`, or store as string in config and parse with
  `time.ParseDuration()` at load time.
- **`/healthz` requests are not logged:** the HTTP logging middleware skips
  `slog.Info` for `GET /healthz` to prevent k8s liveness/readiness probe traffic
  from spamming logs. The endpoint still responds normally — only the log line
  is suppressed. If you expect to see probe traffic in logs for debugging, hit
  any other path or check the endpoint directly with `curl`.
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
  API for additional consumers of the same stream.
- **PAT mode requires specific permissions:** when `boards.git_auth_mode: pat`,
  the fine-grained PAT must have `Contents: Read and write` on the boards repo
  **and** `Issues: Read-only` on each project repo referenced in `.board.yaml`
  that has `github.import_issues: true`. Additionally, `boards.git_remote_url`
  must start with `https://` — SSH URLs are rejected at startup when PAT mode is
  active. PAT mode only works with GitHub (github.com or GHEC/GHES); use SSH for
  other git hosts.
