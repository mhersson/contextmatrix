# Gotchas

- **YAML frontmatter parsing:** use `bytes.SplitN(content, []byte("---"), 3)` to
  split. Element 0 is empty (before first `---`), element 1 is YAML, element 2
  is body. Handle `\r\n` line endings.
- **`next_id` atomicity:** since the server is single-process, a mutex on
  `ProjectConfig` is sufficient. Multi-instance would need file locking.
- **`go-git` performance:** fine for ContextMatrix boards (small files, <10k
  cards). If it becomes an issue, shell out to `git` binary.
- **SSE headers:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `Connection: keep-alive`. Must call `Flusher.Flush()` after each event.
- **Frontend embed:** `//go:embed web/dist/*` in `main.go`. Must build frontend
  _before_ building Go binary. SPA routing requires a fallback to `index.html`
  for all non-API, non-file routes.
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
- **MCP prompts + card context:** prompt handlers return delegation wrappers
  (not raw skill content). The `get_skill` tool fetches card context at
  execution time, calling the service layer in-process. The MCP handler and HTTP
  API share the same `CardService` instance. The `## Agent Configuration`
  section is stripped from all skill content delivered via `get_skill` and
  `complete_task` — the required model is returned as a separate `model` field.
