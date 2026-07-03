# REST API Reference

```text
GET    /api/projects
POST   /api/projects                                     # create project
GET    /api/projects/{project}
PUT    /api/projects/{project}                            # update project config
DELETE /api/projects/{project}                            # delete project (requires 0 cards)

GET    /api/projects/{project}/cards            ?state=&type=&label=&agent=&parent=&priority=&external_id=&vetted=&limit=&cursor=
POST   /api/projects/{project}/cards
GET    /api/projects/{project}/cards/{id}
PUT    /api/projects/{project}/cards/{id}
PATCH  /api/projects/{project}/cards/{id}
DELETE /api/projects/{project}/cards/{id}

POST   /api/projects/{project}/cards/{id}/claim      # agent identity from X-Agent-ID header
POST   /api/projects/{project}/cards/{id}/release     # agent identity from X-Agent-ID header
# heartbeat, log, context, usage, and report-push have no REST endpoint — the
# MCP tools (`heartbeat`, `add_log`, `get_task_context`, `report_usage`,
# `report_push`) are the only interface; agents never call REST directly.

GET    /api/projects/{project}/branches               # list branches from project's GitHub repo
GET    /api/projects/{project}/usage                  # aggregated token usage
GET    /api/projects/{project}/dashboard              # project dashboard metrics
GET    /api/projects/{project}/activity   ?limit=     # flattened activity-log feed (newest first; cap 500)
POST   /api/projects/{project}/recalculate-costs      # recalculate token costs

POST   /api/projects/{project}/cards/{id}/run         # trigger remote execution (human-only)
POST   /api/projects/{project}/cards/{id}/stop        # stop running task (human-only)
POST   /api/projects/{project}/cards/{id}/message     # send chat message to running container (human-only)
POST   /api/projects/{project}/cards/{id}/promote     # promote interactive session to autonomous (human-only)
POST   /api/projects/{project}/stop-all               # stop all running tasks (human-only)
POST   /api/runner/status                              # backend callback at derived /api/<name>/status; per-backend HMAC key
POST   /api/runner/skill-engaged                       # skill-engaged callback at derived /api/<name>/... (HMAC-signed; task-backend required)
GET    /api/runner/task-skills-source                  # agent task-skills {git_remote_url, ref} pointer at derived /api/<name>/... (HMAC-signed; task-backend required)
GET    /api/runner/health                              # proxied runner /health (capacity meter; 2s cached; fixed path)
GET    /api/runner/logs?project=&card_id=              # SSE log stream (card-scoped or project-scoped; fixed path; task-backend required)
GET    /api/v1/cards/{project}/{id}/autonomous         # runner-only autonomous flag read (HMAC-signed; task-backend required)
# /api/agent/* — callback path when the agent entry is the active task backend
# /api/chat/*  — reserved for contextmatrix-chat (not yet released)

GET    /api/chats                                      ?project=&status=&created_by=&limit=
POST   /api/chats                                      # create a new chat session (cold)
GET    /api/chats/models                               # chat model picker source (config|openrouter)
GET    /api/chats/{id}
PATCH  /api/chats/{id}                                 # rename a session
DELETE /api/chats/{id}                                 # delete session and transcript
POST   /api/chats/{id}/open                            # start (or reattach to) the chat container
POST   /api/chats/{id}/end                             # stop the container; flip to cold
POST   /api/chats/{id}/clear                           # clear runner context + re-prime + mark transcript
POST   /api/chats/{id}/messages                        # send a user message into the active container
GET    /api/chats/{id}/messages                        ?since_seq=&limit=    # transcript bootstrap
GET    /api/chats/{id}/stream                          ?since_seq=           # SSE stream of new entries

POST   /api/sync                                      # trigger git sync
GET    /api/sync                                       # sync status

GET    /api/task-skills                                # list available task skill names
GET    /api/app/config                                 # server-side app config (theme/palette/version)

GET    /api/events?project=                           # SSE stream
GET    /healthz                                        # liveness probe (shallow)
GET    /readyz                                        # readiness probe (dependency-checked)

POST   /mcp                                            # MCP Streamable HTTP (Bearer auth; when MCP api key configured)
GET    /mcp                                            # MCP Streamable HTTP SSE channel
DELETE /mcp                                            # MCP Streamable HTTP session close
```

**Admin/debug server:** when `admin_port` is configured (non-zero), a separate
HTTP server binds to `admin_bind_addr` (default `127.0.0.1`) and serves:

- `GET /metrics` — Prometheus text exposition format.
- `GET /debug/pprof/*` — Go runtime profiling (heap, goroutine, profile, etc.).

Neither endpoint is exposed on the main listener. The admin listener has no
built-in authentication — keep it loopback-only, or gate it with a firewall /
NetworkPolicy / service-mesh rule.

**Agent identification:** `X-Agent-ID` header is the **sole** source of agent
identity. It is required on the agent endpoints (`/claim`, `/release`) and on
any mutation of a claimed card — there the header value must match
`assigned_agent` (403 on mismatch). It
is also used to gate human-only fields and human-only endpoints (`/run`,
`/stop`, `/message`, `/promote`, `/stop-all`): those require an `X-Agent-ID`
value beginning with `human:`. Read endpoints, project CRUD,
sync, branches, app config, task-skills, healthz, and readyz do not require the
header. Request bodies on agent endpoints do not carry an `agent_id` field; it
is silently ignored if present.

**Identity is a tag, not auth.** ContextMatrix is single-tenant and has no auth
layer below `X-Agent-ID`; spoofing it accomplishes nothing because there is no
permission gradient to escalate into. The `human:` prefix gates workflow
contracts (only humans promote), not security boundaries. The web UI generates
a per-browser identity (`human:web-<8 hex chars>`) and never prompts the
operator for a username. Routes that act on behalf of the web UI fall back to
`human:web` or `human:api` when no header is present — intentional, because
the UI is the only legitimate caller.
See § Trust model in `CLAUDE.md`.

**CSRF protection:** every state-changing request on the main listener must
carry `X-Requested-With: contextmatrix`. The web UI sets this header on every
non-GET fetch in `web/src/api/client.ts`. Cross-origin browsers cannot set
custom headers on a "simple request" without a CORS preflight, and the server
serves no permissive CORS for state-changing routes — a missing header is
therefore a strong cross-origin signal and the request is rejected with 403
`BAD_REQUEST`. Exempt paths:

- `GET` / `HEAD` / `OPTIONS` on any route (read-only).
- `/api/runner/*`, `/api/agent/*`, `/api/chat/*` — backend callback paths,
  authenticated via per-backend HMAC; not browser paths.
- `/mcp` — Bearer-authed MCP endpoint.
- `/healthz`, `/readyz` — probe endpoints.

The guard sits just outside the mux; any new state-changing route must opt in to
the guard by _not_ adding itself to the exempt list.

**Request correlation:** every response carries an `X-Request-ID` header. If the
client sends an `X-Request-ID` matching `[A-Za-z0-9._-]{1,128}` it is echoed;
otherwise the server generates a UUID. The same id is emitted as the
`request_id` attribute on every structured log line the request produces.

**Error response format:**

```json
{
  "error": "invalid state transition",
  "code": "INVALID_TRANSITION",
  "details": "cannot transition from 'todo' to 'done'; valid targets: [in_progress]"
}
```

**Response codes:**

- 200: success (GET, PUT, PATCH; also `POST /claim`, `/release`,
  `/stop-all`, `/api/runner/status`,
  `POST /api/chats/{id}/open`, `POST /api/chats/{id}/end`,
  `GET /api/v1/cards/.../autonomous`)
- 201: created (`POST /api/projects`, `POST /api/projects/{p}/cards`,
  `POST /api/chats`)
- 202: accepted — async endpoint kicked off background work (`POST /run`,
  `/stop`, `/message`, `/promote`, chat `/messages`)
- 204: deleted (DELETE)
- 400: malformed input (bad JSON, missing/bad query param, unknown filter value,
  missing CSRF header) — emitted with code `BAD_REQUEST`
- 403: agent mismatch (wrong agent trying to modify claimed card), unvetted card
  claim attempt (`CARD_NOT_VETTED`), agent attempting a human-only field
  mutation (`HUMAN_ONLY_FIELD`), HMAC signature / timestamp invalid on a
  runner-side endpoint (`INVALID_SIGNATURE`)
- 404: card, project, chat session, or referenced parent not found —
  parent-not-found uses code `PARENT_NOT_FOUND`
- 409: conflict (invalid transition, card already claimed, already-running
  runner task → `RUNNER_CONFLICT`)
- 413: request body / chat message exceeds the size cap (`CONTENT_TOO_LARGE`)
- 422: semantic validation error — mutation body references an unknown type,
  state, priority, or invalid autonomous combination. Emitted with code
  `VALIDATION_ERROR`. **Not** used for 400-class failures.
- 429: concurrent chat cap reached (`TOO_MANY_CHATS`)
- 502: runner host unreachable (`RUNNER_UNAVAILABLE`)
- 503: no task backend configured (`RUNNER_DISABLED`), sync disabled
  (`SYNC_DISABLED`), or `/readyz` dependency check failed

**Error code / HTTP status mapping (selected):**

| Code                      | HTTP    | Meaning                                                       |
| ------------------------- | ------- | ------------------------------------------------------------- |
| `BAD_REQUEST`             | 400     | malformed input / unknown filter value / CSRF missing         |
| `PROJECT_NOT_FOUND`       | 404     | project slug does not exist                                   |
| `CARD_NOT_FOUND`          | 404     | card ID does not exist in the project                         |
| `PARENT_NOT_FOUND`        | 404     | referenced parent card does not exist                         |
| `CHAT_NOT_FOUND`          | 404     | chat session ID does not exist                                |
| `VALIDATION_ERROR`        | 422     | mutation body semantically invalid                            |
| `INVALID_MODEL`           | 400     | chat `model` not in the active model source (config allowlist, endpoint list, or CM's vendor-screened OpenRouter catalog) |
| `RUNNER_CONFLICT`         | 409     | card already queued/running                                   |
| `RUNNER_DISABLED`         | 503/403 | no task backend configured globally (503) or disabled for the project (403) |
| `RUNNER_UNAVAILABLE`      | 502     | runner webhook failed (host unreachable)                      |
| `RUNNER_NOT_RUNNING`      | 409     | card is not currently running                                 |
| `REVIEW_ATTEMPTS_CAPPED`  | 409     | review attempts limit reached                                 |
| `INVALID_SIGNATURE`       | 403     | HMAC signature or `X-Webhook-Timestamp` missing / expired     |
| `TOO_MANY_CHATS`          | 429     | configured `chat.max_concurrent` cap reached                  |
| `CONTENT_TOO_LARGE`       | 413     | message / request body exceeds the size cap                   |
| `PROTECTED_BRANCH`        | 403     | MCP `report_push` targeted `main` / `master`                  |
| `NO_GITHUB_REPO`          | 404     | project `repo` is not a GitHub URL                            |
| `SYNC_DISABLED`           | 503     | sync trigger with no remote configured                        |
| `SYNC_ERROR`              | 500     | sync trigger raised an error                                  |

**`APIError.details` sanitization:** downstream error strings that look like
go-git transport errors, ssh/exec failures, or absolute filesystem paths are
replaced with stable short labels (`"git remote unreachable"`,
`"git operation failed"`, `"filesystem error"`) before being returned to
clients. The raw error is always logged server-side with the request's
`request_id` so operators can still investigate.

**Error codes relevant to vetting:**

| Code               | HTTP | When                                                                                                                                               |
| ------------------ | ---- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CARD_NOT_VETTED`  | 403  | A non-human agent calls `POST /claim` on a card with `source != null && vetted == false`.                                                          |
| `HUMAN_ONLY_FIELD` | 403  | An agent without `human:` prefix attempts to set `autonomous`, `use_opus_orchestrator`, `feature_branch`, `create_pr`, `vetted`, or `base_branch`. |

## Health Endpoints

### GET /healthz

Shallow liveness probe. Always returns `200 OK` with JSON body `{"status":"ok"}`
(`Content-Type: application/json`) as long as the process is running. No
dependency checks are performed.

Use this as a k8s `livenessProbe` target (or equivalent). Do not use it to gate
traffic — a `200` from `/healthz` only means the process has not crashed.

```bash
curl http://localhost:8080/healthz
# → {"status":"ok"}
```

### GET /readyz

Dependency-checked readiness probe. Runs three checks with a 500 ms timeout:

| Check         | What it tests                                                                                                                                                                                                                         |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `store`       | `ListProjects` succeeds (boards directory is readable)                                                                                                                                                                                |
| `git`         | `CurrentBranch` resolves (git manager is initialised)                                                                                                                                                                                 |
| `session_log` | always reports `ok: true`. A nil session-log manager simply means the runner is disabled (still healthy); a non-nil manager means it is operational. The check is included for forward compatibility but never fails the probe today. |

Returns **200** when all checks pass, **503** when any check fails.

**Response body (200):**

```json
{
  "status": "ok",
  "checks": [
    { "name": "store", "ok": true },
    { "name": "git", "ok": true },
    { "name": "session_log", "ok": true }
  ]
}
```

**Response body (503):**

```json
{
  "status": "degraded",
  "checks": [
    {
      "name": "store",
      "ok": false,
      "error": "open /data/boards: permission denied"
    },
    { "name": "git", "ok": true },
    { "name": "session_log", "ok": true }
  ]
}
```

Use this as a k8s `readinessProbe` target. Kubernetes operators should point:

- `readinessProbe` → `GET /readyz`
- `livenessProbe` → `GET /healthz`

```bash
curl http://localhost:8080/readyz
```

```yaml
# Kubernetes probe example
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 30
```

### Card list query parameters

| Parameter     | Values           | Description                                                                                    |
| ------------- | ---------------- | ---------------------------------------------------------------------------------------------- |
| `state`       | state name       | Filter by card state                                                                           |
| `type`        | type name        | Filter by card type                                                                            |
| `label`       | label string     | Filter cards that have this label                                                              |
| `agent`       | agent ID         | Filter by `assigned_agent`                                                                     |
| `parent`      | card ID          | Filter by parent card                                                                          |
| `priority`    | priority name    | Filter by priority                                                                             |
| `external_id` | external ID      | Filter by `source.external_id` (idempotent import check)                                       |
| `vetted`      | `true` / `false` | Filter by `vetted` field. `?vetted=false` lists unvetted external cards awaiting human review. |
| `limit`       | 1–2000           | Maximum items in the response page. Default `500`. Out-of-range values return 400.             |
| `cursor`      | opaque string    | Page continuation token from the previous response's `next_cursor`. Opaque to clients.         |

### Card list response envelope

`GET /api/projects/{project}/cards` returns a JSON object (not a bare array):

```json
{
  "items": [{ "id": "PROJ-001", "...": "..." }],
  "next_cursor": "UFJPSi0wMDE",
  "total": 1234
}
```

- `items` — page of cards, ordered by ID ascending. Always present (may be
  `[]`).
- `next_cursor` — opaque base64url token; pass back in `?cursor=` to fetch the
  next page. Omitted when the current page is the last page.
- `total` — total un-filtered card count for the project. Emitted **only on the
  first page** (when the request has no `cursor`). Callers can use it for
  "showing X of Y" indicators even while a filter is active.

Cursors encode the last card ID of the page and are stable across filter changes
— callers must treat them as opaque. Invalid cursors (not valid base64url)
return 400 `BAD_REQUEST`.

Ordering is by card ID ascending. The server sorts before slicing, so walking
`next_cursor` to exhaustion is guaranteed to visit every matching card exactly
once even though the underlying store iterates a map.

```bash
# First page — 1 item, includes total.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1"
# → {"items":[{"id":"ALPHA-001", ...}],"next_cursor":"QUxQSEEtMDAx","total":3}

# Follow-up pages use cursor.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1&cursor=QUxQSEEtMDAx"
# → {"items":[{"id":"ALPHA-002", ...}],"next_cursor":"QUxQSEEtMDAy"}

# Last page — next_cursor omitted.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1&cursor=QUxQSEEtMDAy"
# → {"items":[{"id":"ALPHA-003", ...}]}
```

## App Endpoints

### GET /api/task-skills

Returns the list of task skills available in the configured `task_skills.dir`.
Each entry has a `name` (the skill directory name) and a `description` (read
from the skill's `SKILL.md` frontmatter). The response is a JSON object with a
`skills` array.

```json
{
  "skills": [
    {
      "name": "documentation",
      "description": "Use when writing or updating documentation files."
    },
    {
      "name": "go-development",
      "description": "Use when implementing or modifying Go source files."
    },
    {
      "name": "python-development",
      "description": "Use when writing or modifying Python source files."
    },
    {
      "name": "typescript-react",
      "description": "Use when writing or updating React or TypeScript component files."
    }
  ]
}
```

Returns `{"skills": []}` if `task_skills.dir` is not configured or the directory
is empty. Used by the Project Settings UI to populate the
`DefaultSkillsSelector`.

### GET /api/app/config

Returns the server-configured application settings. Unauthenticated — safe for
public read. Called by the frontend on startup to determine which color palette
to apply.

**Response:**

```json
{
  "theme": "everforest",
  "version": "v0.42.0",
  "task_backend": "runner",
  "favorites": { "complex": ["anthropic/claude-opus-4.8"] }
}
```

`theme` is one of `"everforest"` (default), `"radix"`, or `"catppuccin"`. The
frontend sets `data-palette` on `<html>` to match the theme value;
`"everforest"` removes the attribute (it is the default CSS block). `version` is
the build version string the binary was compiled with; it is always present and
is empty when the binary is built without the version ldflag. `task_backend`
is the active task-backend name (`"runner"`, `"agent"`, or `""` when none is
configured). `favorites` maps each tier to its operator-configured preferred
model slugs (the leaderboard "favorites as pin presets" surface); the field is
omitted entirely when no favorites are configured.

```bash
curl http://localhost:8080/api/app/config
# → {"theme":"everforest","version":"v0.42.0","task_backend":"runner"}
```

### GET /api/models

Model catalog for the card model-pin pickers. Returns the vendor-screened
OpenRouter list (source `openrouter`) or the endpoint's served list (source
`endpoint`) from the server-side catalog cache; `source: "none"` with an empty
list when no catalog builder is configured. Independent of the chat mode —
pins are an agent-backend concern.

```json
{ "source": "openrouter", "models": [ { "id": "anthropic/claude-sonnet-4.5", "max_tokens": 200000 } ] }
```

Card model pins (`model_orchestrator` / `model_coder` / `model_reviewer` on
card create, update, and patch) are validated against this same served set:
setting a pin to a slug outside it returns 422 `VALIDATION_ERROR` ("model pin
not in catalog"). Only changed values are checked — a pre-existing pin never
blocks unrelated card edits — and an empty or unfetched catalog disables
validation entirely (fail-open).

## Project Endpoints

### POST /api/projects

Create a new project. Either `name` (slug) or `display_name` (human-readable)
must be provided; both may be provided together.

**Request body:**

```json
{
  "name": "epic-planner",
  "display_name": "Epic Planner",
  "prefix": "EPIC",
  "repo": "git@github.com:org/epic-planner.git",
  "states": ["todo", "in_progress", "review", "done", "stalled", "not_planned"],
  "types": ["task", "bug"],
  "priorities": ["low", "medium", "high"],
  "transitions": {
    "todo": ["in_progress", "not_planned"],
    "in_progress": ["review", "todo"],
    "review": ["done", "in_progress"],
    "done": ["todo"],
    "stalled": ["todo"],
    "not_planned": ["todo"]
  }
}
```

**Field rules:**

| Field          | Required?   | Description                                                                                                                                                  |
| -------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `name`         | conditional | Slug — filesystem directory name, URL path segment, API identifier. Must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Auto-derived from `display_name` when omitted. |
| `display_name` | conditional | Human-readable project name. May contain spaces and any printable characters. Stored in `.board.yaml`; shown in the UI sidebar.                              |
| `prefix`       | required    | Card ID prefix (e.g. `EPIC` → `EPIC-001`).                                                                                                                   |

At least one of `name` or `display_name` is required (400 if both are absent).

**Slug auto-derivation:** when `name` is omitted, the server derives it from
`display_name` by lowercasing and collapsing runs of non-alphanumeric characters
to hyphens (e.g. `"Epic Planner"` → `"epic-planner"`). A 409 is returned if the
derived or explicit slug already exists as a project directory.

**Response:** 201 Created with the full `ProjectConfig` object, including the
stored `name` and `display_name`.

### GET /api/projects / GET /api/projects/{project}

List all projects or get a single project by slug. Both responses include
`display_name` when set.

```json
{
  "name": "epic-planner",
  "display_name": "Epic Planner",
  "prefix": "EPIC",
  "next_id": 1,
  "states": ["..."],
  "..."
}
```

Existing projects without `display_name` omit the field; clients should fall
back to displaying `name`.

### PUT /api/projects/{project}

Update the project configuration. The update body is a **subset** of
`POST /api/projects` — `name`, `display_name`, and `prefix` are immutable and
not accepted here. Two extra fields are available: `github` (GitHub import
configuration) and `default_skills` (project-wide task-skill fallback).

**Accepted fields:**

```json
{
  "repo": "git@github.com:org/epic-planner.git",
  "states": ["todo", "in_progress", "review", "done", "stalled", "not_planned"],
  "types": ["task", "bug"],
  "priorities": ["low", "medium", "high"],
  "transitions": {
    "todo": ["in_progress"],
    "in_progress": ["review", "todo"],
    "...": "..."
  },
  "github": { "...": "..." },
  "default_skills": ["go-development", "documentation"]
}
```

To rename a project or change its prefix, recreate it via `POST /api/projects`.

**`default_skills` field** — three-state semantics for the project-wide
task-skill fallback:

| Value                                 | Meaning                                                           |
| ------------------------------------- | ----------------------------------------------------------------- |
| field omitted / `null`                | Clear: runner mounts the full curated task-skills set             |
| `[]` (empty array)                    | Mount no task skills for cards without an explicit `skills` field |
| `["go-development", "documentation"]` | Constrain cards without explicit `skills` to this list            |

Each name in `default_skills` must exist in the configured `task_skills.dir` —
unknown names return 400 `VALIDATION_ERROR`. (This is the one place
`VALIDATION_ERROR` is paired with a 400 status; the error table above maps it to
422 for mutation bodies that are semantically invalid.) A card's own `skills`
field (including explicit empty) always overrides the project default.

The Project Settings UI exposes this as the **Default task skills** selector
with "Mount full set" / "Mount no skills" / "Constrain to selected skills" radio
buttons.

Returns 200 with the updated `ProjectConfig`.

### GET /api/projects/{project}/branches

Returns a JSON array of branch name strings for the project's GitHub repository.
Used by the card editor to populate the base branch dropdown.

Returns 404 with `NO_GITHUB_REPO` if the project's `repo` field is not a GitHub
URL. If GitHub credentials are missing or the upstream API call fails the
handler currently returns 500 `INTERNAL_ERROR` with the underlying error logged
server-side.

```json
["main", "develop", "release/v2", "feat/some-branch"]
```

**Error codes:**

| Code             | HTTP | When                                                     |
| ---------------- | ---- | -------------------------------------------------------- |
| `NO_GITHUB_REPO` | 404  | Project `repo` is not a GitHub repository URL            |
| `INTERNAL_ERROR` | 500  | GitHub branch fetch failed (auth, network, upstream API) |

### GET /api/projects/{project}/usage

Returns aggregated token usage across all cards in a project.

```json
{
  "prompt_tokens": 45000,
  "completion_tokens": 12000,
  "cache_read_tokens": 380000,
  "cache_creation_tokens": 12000,
  "estimated_cost_usd": 0.315,
  "card_count": 8
}
```

`cache_read_tokens` and `cache_creation_tokens` are zero-valued on projects that
have no cache activity and are always present in the response (not `omitempty`).

### GET /api/projects/{project}/dashboard

Returns dashboard metrics for a project.

```json
{
  "state_counts": { "todo": 3, "in_progress": 2, "done": 5 },
  "active_agents": [
    {
      "agent_id": "claude-7a3f",
      "card_id": "ALPHA-003",
      "card_title": "...",
      "since": "...",
      "last_heartbeat": "..."
    }
  ],
  "total_cost_usd": 0.315,
  "total_cost_usd_last_30d": 0.284,
  "total_cost_usd_prior_30d": 0.241,
  "cost_series_30d": [0.003, 0.012, 0.008, "... (30 daily buckets, oldest first)"],
  "cards_completed_today": 2,
  "cards_completed_last_7d": 9,
  "cards_completed_prior_7d": 6,
  "metric_series": {
    "active_agents": [0, 1, 1, 2, 2, 1, 3, 2],
    "in_flight":     [1, 2, 2, 3, 3, 2, 4, 3],
    "stalled":       [0, 0, 1, 1, 0, 0, 0, 0],
    "shipped":       [1, 0, 2, 1, 1, 2, 1, 2]
  },
  "agent_costs": [
    {
      "agent_id": "claude-7a3f",
      "prompt_tokens": 30000,
      "completion_tokens": 8000,
      "estimated_cost_usd": 0.21,
      "card_count": 5
    }
  ],
  "model_costs": [
    {
      "model": "claude-sonnet-4-5",
      "prompt_tokens": 25000,
      "completion_tokens": 6000,
      "estimated_cost_usd": 0.18,
      "card_count": 4
    }
  ],
  "card_costs": [
    {
      "card_id": "ALPHA-003",
      "card_title": "...",
      "assigned_agent": "claude-7a3f",
      "prompt_tokens": 5000,
      "completion_tokens": 1200,
      "estimated_cost_usd": 0.033
    }
  ],
  "chat_cost_usd_last_30d": 0.142,
  "chat_cost_usd_prior_30d": 0.098,
  "chat_cost_series_30d": [0.001, 0.004, 0.009, "... (30 daily buckets, oldest first)"]
}
```

`assigned_agent` is omitted when no agent currently owns the card.

`model_costs` aggregates token usage and cost per model across the project.
Cards whose token-usage records have an empty `model` string are bucketed
under `"unknown"`. Each card is attributed to its most-recently-used model
(cards that used multiple models show under the last one).

`cards_completed_last_7d` counts cards whose `updated` falls inside the
trailing 7-day window ending at "now"; `cards_completed_prior_7d` counts the
preceding 7-day window (used by the UI to render a week-over-week delta).

`total_cost_usd_last_30d` is the sum of `EstimatedCostUSD` for cards whose
`updated` timestamp falls within the last 30 days. `total_cost_usd_prior_30d`
covers the preceding 30-day window (days 30–60 ago), used by the UI to render
a period-over-period delta on the "Cost · 30d" tile. `cost_series_30d` is a
30-element daily bucket array (oldest first, today last) bucketed by `updated`
day; each card's cumulative cost is attributed to its last-touch day. The
server always emits all three fields, alongside the all-time `total_cost_usd`.

`chat_cost_usd_last_30d`, `chat_cost_usd_prior_30d`, and `chat_cost_series_30d`
are **server-wide** aggregates (not per-project) that ride on the per-project
dashboard payload for fan-out convenience. They sum `estimated_cost_usd` across
all chat sessions bucketed by `last_active`, aligned on UTC midnight so today's
partial day occupies `chat_cost_series_30d[29]`. **Important caching behavior:**
`chat.Manager.GetChatCostSummary` caches the result for 30 seconds
(`chatCostCacheTTL`). When multiple projects are fetched in quick succession (the
All Projects view fans out one request per project), all responses within the same
30-second window return identical chat-cost values. Frontend aggregation picks the
**first response with a numeric `chat_cost_usd_last_30d`** (i.e. `typeof ... ===
'number'`) — not the first non-zero value — so a genuinely-zero last30d with
non-zero prior30d still propagates correctly. `chat_cost_series_30d` is omitted
when empty; treat a missing value as `0`.

`metric_series` is an 8-sample daily window (oldest first, today last) for
each tile on the board's metrics ribbon. Each slice always has exactly 8
entries. `shipped` is bucketed by `updated` on cards in the `done` state;
the other three are reconstructed by walking each card's `state_changed`
activity-log entries. The `active_agents` series counts cards whose
reconstructed end-of-day state is `in_progress`/`review` **and** which
currently have an assigned agent (claim history isn't tracked, so per-day
agent presence is approximate). Cards that pre-date state-change logging
fall back to their current `state` for the whole window.

**Prometheus counters related to chat cost (served on the admin `/metrics` endpoint):**

| Counter                                           | Labels  | Meaning                                                                                                                                |
| ------------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `contextmatrix_chat_usage_unknown_model_total`    | `model` | Incremented when `handleUsageEntry` prices a frame whose model is not in `token_costs`. Tokens accumulate normally; cost stays `$0` for that frame. |
| `contextmatrix_chat_cost_summary_errors_total`    | —       | Incremented when `GetChatCostSummary` fails inside `GetDashboard`. The dashboard still renders with zero-valued chat-cost fields on each error. |

### GET /api/projects/{project}/activity

Returns a chronological flat feed of activity-log entries across every card
in the project. Used by the board's NowRail "Activity" section to backfill
entries older than the page load (SSE delivers everything from page load
forward).

Query parameters:

- `limit` (optional, default 50, max 500) — maximum number of entries to
  return. Invalid values (non-integer or `<= 0`) return 400.

Response envelope mirrors `/cards` — uses `items` rather than a bare array:

```json
{
  "items": [
    {
      "agent": "claude-7a3f",
      "action": "claimed",
      "message": "",
      "card_id": "ALPHA-003",
      "ts": "2026-05-17T12:34:56Z"
    }
  ]
}
```

Entries are sorted newest-first by `ts`. The feed is rolling (no cursor):
clients receive at most `limit` entries and refresh by re-fetching.

### POST /api/projects/{project}/recalculate-costs

Recalculate estimated costs for all cards in a project using current
`token_costs` rates. Requires `default_model` for cards that have tokens but no
model recorded.

Cards with a `usage_breakdown`: every bucket with `cost_source: estimated` is
re-priced from the current rates (stale prices are corrected); buckets with
`cost_source: actual` are never modified. Legacy cards without a breakdown:
fill-missing-only — cards with non-zero tokens but $0 cost get a cost; cards
with an existing cost are not modified.

```json
{ "default_model": "claude-sonnet-4-6" }
```

Returns:

```json
{ "cards_updated": 12, "total_cost_recalculated": 0.847 }
```

## Sync Endpoints

### POST /api/sync

Trigger a git pull on the boards repository. Returns 503 if sync is disabled (no
remote configured).

### GET /api/sync

Returns current sync status.

```json
{
  "last_sync_time": "2026-04-05T12:00:00Z",
  "last_sync_error": "",
  "syncing": false,
  "enabled": true
}
```

| Field             | Type               | Description                                                   |
| ----------------- | ------------------ | ------------------------------------------------------------- |
| `last_sync_time`  | RFC 3339 / `null`  | Timestamp of the last completed sync attempt; `null` if none. |
| `last_sync_error` | string (omitempty) | Error message from the most recent failed sync.               |
| `syncing`         | bool               | `true` while a sync is in flight.                             |
| `enabled`         | bool               | Whether automatic sync is enabled in config.                  |

## Runner Endpoints

See [`docs/remote-execution.md`](remote-execution.md) for the full webhook
protocol, HMAC signing details, and runner configuration.

### POST /api/projects/{project}/cards/{id}/run

Trigger remote execution for a card. Human-only (rejects `X-Agent-ID` without
`human:` prefix). Requires card to be in `todo` state and a task backend
configured globally + per-project remote execution enabled. The `autonomous`
flag is **not** required.

Accepts an optional JSON body:

```json
{ "interactive": true }
```

When `interactive` is `true`, the container starts in Human-in-the-Loop (HITL)
mode — the runner writes a priming stream-json user message to the container's
stdin after start, which instructs Claude to invoke `get_skill(create-plan)`
immediately. The user provides approval at the skill's built-in gates (plan
approval, subtask execution decision, review) via the chat input.

Regardless of `interactive`, `feature_branch` and `create_pr` are automatically
enabled on the card for all run triggers (both autonomous and HITL).

Returns **202 Accepted** with the updated card (`runner_status: "queued"`). The
response is returned as soon as the runner webhook is accepted — the runner then
provisions the container asynchronously.

### POST /api/projects/{project}/cards/{id}/message

Send a chat message to a container running in interactive mode. Human-only.
Requires `runner_status: "running"`.

```json
{ "content": "Please focus on the authentication module first." }
```

- 422 if `content` is empty
- 413 `CONTENT_TOO_LARGE` if `content` exceeds 8 KiB
- 409 `RUNNER_NOT_RUNNING` if the card is not running

Returns 202 with:

```json
{ "ok": true, "message_id": "uuid-v4-string" }
```

### POST /api/projects/{project}/cards/{id}/promote

Promote an interactive session to autonomous mode. Human-only. Requires
`runner_status: "running"`.

The endpoint performs two steps in order:

1. Calls `CardService.PromoteToAutonomous` to flip the card's `autonomous` flag
   to `true`, append an activity log entry (`action=promoted`), commit the
   change to the boards git repository, and publish a `CardUpdated` SSE event.
   This step is **idempotent** — if the card is already autonomous, it returns
   the current card without writing a new log entry or commit.
2. Sends a `/promote` webhook to the runner. The runner then writes a canned
   stdin message to the container instructing Claude Code to check the card with
   `get_card` at its next gate and continue on the autonomous branch.

`feature_branch` and `create_pr` are also set to `true` if not already enabled.

**Error responses:**

- 403 `HUMAN_ONLY_FIELD` if the caller is not a human agent
- 409 `RUNNER_NOT_RUNNING` if `runner_status` is not `"running"`
- 409 `INVALID_TRANSITION` if the card is in a terminal state (`done` or
  `not_planned`) — the flag flip itself is rejected before any webhook is sent
- 502 `RUNNER_UNAVAILABLE` if the runner webhook fails — the autonomous flag
  flip is **not** reverted; the card is permanently promoted

Returns **202 Accepted** with the updated card. The idempotent short-circuit
(card already autonomous) also returns 202 with the current card state and no
new log entry.

```bash
curl -X POST http://localhost:8080/api/projects/my-project/cards/PROJ-042/promote \
  -H "X-Agent-ID: human:alice"
```

### POST /api/projects/{project}/cards/{id}/stop

Stop a running remote execution. Human-only. Sends kill webhook to runner.
Returns **202 Accepted** with the updated card (`runner_status: "killed"`).

### POST /api/projects/{project}/stop-all

Stop all running remote executions in a project. Human-only. Returns
`{ "affected_cards": ["PROJ-001", "PROJ-003"] }`.

### GET /api/runner/health

Browser-facing fixed path — always at `/api/runner/health` regardless of
which task backend is configured. Proxies a `GET /health` to the configured
task backend and returns the parsed shape. Used by the board's NowRail to
render the capacity meter (`max_concurrent` is the runner-global cap, not a
per-project value).

Returns:

```json
{
  "ok": true,
  "running_containers": 2,
  "max_concurrent": 4
}
```

- 503 `RUNNER_DISABLED` when no task backend is configured.
- 502 `RUNNER_UNAVAILABLE` when a task backend is configured but the probe
  fails (timeout, transport error, non-2xx response). Upstream error
  details are not surfaced in the response body — the underlying error
  is logged server-side. Callers should fail soft (hide capacity).

Probe results are cached server-side for ~2 seconds so concurrent
browser tabs do not each fire a fresh probe. Probes use a tighter
upstream timeout (3 s) than the runner client's default to keep the
endpoint responsive during a runner outage.

### GET /api/runner/logs

SSE log stream. Browser-facing fixed path — always at `/api/runner/logs`
regardless of which task backend is configured. Only available when a task
backend is configured (runner or agent entry enabled in the backends map). Not
authenticated — the browser connects directly; HMAC signing is performed
server-side toward the runner.

**Query parameters:**

| Parameter | Required    | Description                                 |
| --------- | ----------- | ------------------------------------------- |
| `project` | recommended | Filter entries to a single project          |
| `card_id` | optional    | Enable card-scoped session mode (see below) |

**Two modes, selected by `card_id`:**

- **Card-scoped** (`?project=P&card_id=X`): connects to the server-side session
  manager. The server first replays all buffered events (snapshot), then tails
  live events. A client that reconnects receives all events from the start of
  the session, including any HITL questions. The session exists from when the
  card enters `running` until a terminal status (`failed`, `killed`,
  `completed`). Returns 204 if the session manager is unavailable.
- **Project-scoped** (`?project=P`, no `card_id`): connects to the server-side
  session manager for the project. The server first replays all buffered project
  events (snapshot), then tails live events — identical replay guarantee as the
  card-scoped path. Used by the Runner Console panel. A reconnecting client
  receives all events buffered since the console was first opened. Returns 204
  if the session manager is unavailable.

**Response:** `Content-Type: text/event-stream`. The server sets
`X-Accel-Buffering: no` on all SSE responses to bypass nginx proxy buffering. A
`: keepalive\n\n` comment is written every 30 seconds per subscription to
survive Cloudflare/nginx idle timeouts (~100 s).

Each normal event carries a JSON payload:

```json
{
  "ts": "2026-04-08T12:34:56.789Z",
  "card_id": "PROJ-042",
  "type": "text",
  "content": "Planning the implementation...",
  "seq": 42
}
```

Marker frames have a distinct shape:

| Frame type | Payload shape                          | Meaning                                                    |
| ---------- | -------------------------------------- | ---------------------------------------------------------- |
| `terminal` | `{"type":"terminal","seq":N}`          | Session ended; no further events                           |
| `dropped`  | `{"type":"dropped","seq":N,"count":N}` | Server ring-buffer overflowed; `count` events were evicted |

`type` for normal events is one of: `text`, `thinking`, `tool_call`,
`stderr`, `system`, `user`.

The connection is closed when the browser disconnects or the session receives a
terminal event.

**Client behaviour (`useRunnerLogs`):**

- Tracks last-seen `seq`; if an incoming `seq > lastSeq + 1`, inserts a
  client-side gap marker (`type: 'gap'`) indicating the number of missing
  events.
- `dropped` frames render as gap markers (not as ordinary log lines).
- `terminal` frames clear `connected` and stop the reconnect loop — no further
  reconnect is attempted after a clean session end.

See [`docs/remote-execution.md`](remote-execution.md) for the full log streaming
architecture, `LogEntry` type details, and session manager configuration.

### POST /api/runner/status

Runner callback endpoint. Mounts at `/api/<name>` derived from the active task
backend's entry name (e.g. `/api/runner` for the runner entry, `/api/agent`
for the agent entry). Requires **both** an `X-Signature-256` header
(HMAC-SHA256, prefixed with `sha256=`, signed with the matching backend's
`api_key`) and an `X-Webhook-Timestamp` header (used for clock-skew rejection).
Missing either header, a malformed signature, or an expired timestamp returns
403 `INVALID_SIGNATURE`. Only registered when a task backend is configured.

Accepts `runner_status` updates (`"running"`, `"failed"`, `"completed"`). The
server-only statuses `"queued"` and `"killed"` are rejected with 422
`VALIDATION_ERROR`.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "runner_status": "running",
  "message": "container started"
}
```

### POST /api/runner/skill-engaged

Runner callback endpoint reporting that the in-container Claude session engaged
a workflow skill. Same HMAC authentication as `/api/runner/status`
(`X-Signature-256` + `X-Webhook-Timestamp`, signed with the backend's `api_key`).
Only registered when a task backend is configured. Used for runner-side
telemetry; the response body is `{"ok": true}`.

### GET /api/v1/cards/{project}/{id}/autonomous

Runner-only read endpoint (fixed path, independent of the derived callback
path). Authenticated with HMAC-SHA256 over an empty body (`X-Signature-256` +
`X-Webhook-Timestamp`, signed with the task backend's `api_key`). Returns the
minimal shape `{"autonomous": <bool>}` so the runner can fail-closed verify a
card's autonomous flag during `/promote` before writing the canned stdin
message. Only registered when a task backend is configured. No other card
fields are exposed on this path.

### GET /api/<name>/task-skills-source

Backend-callback endpoint at the derived path (`/api/runner/task-skills-source`
or `/api/agent/task-skills-source`). Authenticated with HMAC-SHA256 over an
empty body (`X-Signature-256` + `X-Webhook-Timestamp`, signed with the task
backend's `api_key`), the same scheme as `/autonomous`. Returns the task-skills
repo pointer the agent backend clones for itself:

```json
{
  "git_remote_url": "https://github.com/org/task-skills.git",
  "ref": "main",
  "token": "ghs_...",
  "token_expires_at": "2026-07-05T13:00:00Z"
}
```

`token` is a clone credential minted from the instance `github.*` credential
(the task-skills repo is instance-scoped, never bound to a project pool
entry). It is best-effort: when minting fails, the response carries only the
pointer and the backend falls back to its own credential. `token_expires_at`
is RFC3339 and absent for PAT-backed credentials (no server-managed TTL —
absent means "do not schedule a refresh"). The chat-backend variant at
`GET /api/chat/task-skills-source` returns the same shape.

Only registered when a task backend is configured.

### GET /api/<name>/git-credentials

Backend-callback endpoint at the derived path (e.g.
`/api/agent/git-credentials`), HMAC-signed like `task-skills-source`. Re-mints
the project-scoped git token mid-run — App installation tokens live ~1h while
card runs can go far longer.

Query parameters: `project` and `card_id` (both required). The card must exist
and have `runner_status: running` — a non-running card gets 409, so the
endpoint is not a free token faucet. The token is minted from the project's
`github_credential` binding (or the instance credential when unbound); a
broken binding fails closed with 409, never the instance credential.

```json
{ "token": "ghs_...", "expires_at": "2026-07-05T13:00:00Z" }
```

`expires_at` is absent for PAT-backed credentials (same omission semantics as
`token_expires_at` above; the key names differ because the two responses are
different shapes — consumers read each endpoint's own key).

Trigger-time minting: `POST .../cards/{id}/run` populates
`TriggerPayload.git_token` / `git_token_expires_at` / `llm_endpoint` the same
way; a broken binding rejects the run with 409, reverts the card's runner
status, and appends a card activity entry.

## Chat Endpoints

Project-agnostic chat sessions that share the runner's worker image but use
long-lived containers instead of card-scoped one-shots. Identity follows the
same `X-Agent-ID` tagging convention as the rest of the API (see § Trust model
in `CLAUDE.md`); the web UI defaults to `human:web` when the header is absent.

### POST /api/chats

Create a new session row. Status starts at `cold`; no container is started yet.

Request body:

```json
{
  "title": "Investigate auth-flow regression",
  "project": "contextmatrix",
  "model": "claude-opus-4-8"
}
```

All three fields are optional. An empty `title` is auto-filled from the first
user message; `project` may be empty for cross-project chats. `model` selects
the model for this session; omit to use the server default. The choice is
persisted on the session row and forwarded to the container on every `/open`.

Model validation depends on which backend serves chat (see
`GET /api/chats/models`):

- **runner serves chat** (`source: "config"`): `model` must be a key from
  `chat.models`; unknown IDs return `400` (`INVALID_MODEL`). Omit to use
  `chat.default_model`. Forwarded as `CM_ORCHESTRATOR_MODEL`.
- **dedicated chat backend serves chat** (`source: "openrouter"`): `model` must
  be in CM's vendor-screened OpenRouter catalog (the same list
  `GET /api/chats/models` serves); unknown slugs return `400`
  (`INVALID_MODEL`). Validation fails open when the catalog has not been
  fetched (cold start, OpenRouter outage). Omit to use
  `backends.chat.default_model`. Forwarded as `CM_MODEL`.
- **OpenAI-compatible endpoint serves chat** (`source: "endpoint"`): `model`
  must be in the endpoint's served model list; unknown slugs return `400`
  (`INVALID_MODEL`). A failed upstream list fetch fails open. Omit to use
  `backends.chat.default_model`.

Response (`201 Created`): the new `ChatSession` row.

### GET /api/chats/models

Tells the New Chat dialog which model picker to render. The `source` field
selects the mode:

- `"config"` — the runner serves chat. `models` is the `chat.models` allowlist
  (native Anthropic slugs, sorted by `id`) and `default` is `chat.default_model`.
- `"openrouter"` — the dedicated chat backend (contextmatrix-chat) serves chat.
  `models` is CM's vendor-screened OpenRouter catalog (`id`/`label` = the
  OpenRouter slug, `max_tokens` = the model's context window), served from the
  server-side catalog cache; empty only when the catalog has not been fetched.
  `default` is `backends.chat.default_model`.
- `"endpoint"` — an OpenAI-compatible endpoint serves chat. `models` is the
  endpoint's served model list (cached server-side); `default` is
  `backends.chat.default_model`.

Response (`source: "config"`):

```json
{
  "source": "config",
  "models": [
    {
      "id": "claude-haiku-4-5-20251001",
      "label": "Haiku 4.5",
      "max_tokens": 200000
    },
    { "id": "claude-opus-4-7", "label": "Opus 4.7", "max_tokens": 1000000 },
    { "id": "claude-opus-4-8", "label": "Opus 4.8", "max_tokens": 1000000 },
    { "id": "claude-sonnet-4-6", "label": "Sonnet 4.6", "max_tokens": 1000000 }
  ],
  "default": "claude-sonnet-4-6"
}
```

Response (`source: "openrouter"`):

```json
{
  "source": "openrouter",
  "models": [
    {
      "id": "anthropic/claude-sonnet-4.5",
      "label": "anthropic/claude-sonnet-4.5",
      "max_tokens": 200000
    }
  ],
  "default": "anthropic/claude-sonnet-4"
}
```

When chat is disabled in config the response is
`{"source": "config", "models": [], "default": ""}`.

### GET /api/chats

List sessions, newest-first by `last_active`. Query parameters:

| Param        | Default | Max  | Effect                                                                           |
| ------------ | ------- | ---- | -------------------------------------------------------------------------------- |
| `project`    | —       | —    | Filter by project name (omit for all)                                            |
| `status`     | —       | —    | Filter by `cold` / `active` / `warm-idle` / `ending`. Unknown values return 400. |
| `created_by` | —       | —    | Filter by agent ID (e.g. `human:web-1a2b3c4d`)                                   |
| `limit`      | `500`   | 5000 | Cap on rows returned; out-of-range values clamp / 400                            |

Response: a JSON array of `Session`. Always `[]`, never `null`.

### GET /api/chats/{id}

Returns the `ChatSession` row. `404` (`CHAT_NOT_FOUND`) if unknown.

Response fields that the UI header consumes:

| Field                       | Type    | Meaning                                                                                                                                    |
| --------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `model`                     | string  | Orchestrator model ID. Set at creation; reused on every `/open`.                                                                           |
| `context_tokens`            | int     | Last `input + cache_read + cache_create` reported by Claude. Updated on every assistant turn.                                              |
| `context_tokens_updated_at` | RFC3339 | Timestamp of the last `context_tokens` update. Zero (`0001-01-01T00:00:00Z`) until the first usage entry.                                  |
| `rehydration_active`        | bool    | `true` between cold-reopen and the agent's `chat_rehydration_complete` call. Drives the "Restoring workspace…" banner. Omitted when false. |
| `prompt_tokens`             | int64   | Cumulative input tokens reported across all usage frames for this session. Omitted when zero.                                              |
| `completion_tokens`         | int64   | Cumulative output tokens. Omitted when zero.                                                                                               |
| `cache_read_tokens`         | int64   | Cumulative cache-read tokens. Omitted when zero.                                                                                           |
| `cache_creation_tokens`     | int64   | Cumulative cache-creation tokens. Omitted when zero.                                                                                       |
| `estimated_cost_usd`        | float64 | Running cost total in USD, computed using the same cache-tier formula as card-scoped `report_usage`. Precision floor ~$0.0001. Omitted when zero. |

### PATCH /api/chats/{id}

Update a session's title.

```json
{ "title": "Renamed: auth-flow regression" }
```

Response: the updated `ChatSession`.

### DELETE /api/chats/{id}

Removes the session and its transcript (FK cascade on `chat_messages`). If the
session is `active` or `warm-idle`, the runner container is ended first.

### POST /api/chats/{id}/open

Transition a cold session to active by starting the chat container. Idempotent
for `active` sessions; reattaches to the existing container for `warm-idle`.
Returns `429` (`TOO_MANY_CHATS`) when the configured `chat.max_concurrent` cap
is reached.

Response: the refreshed `ChatSession` (now with `status: active` and a
`container_id`).

### POST /api/chats/{id}/end

End the session: closes the container's stdin and force-stops it. Status flips
to `cold`; `container_id` is cleared.

Response (`200 OK`): the refreshed `ChatSession` in the cold state.

### POST /api/chats/{id}/clear

Clear the runner's working memory in place without ending the session. The
server sends `"/clear"` to the runner, re-primes the session with the
chat-mode primer (if configured), marks every prior transcript row with
`rehydration_phase = true` so it is excluded from future cold-open resume
payloads, and appends a divider row (`role: system`, `content: "Context
cleared"`, `kind: "divider"`) that the UI renders as a horizontal rule.
The divider is broadcast on the SSE wire AND persisted, so a page reload
still shows the rule in the transcript.

Request: empty JSON body (`{}`). CSRF-gated; UI-only.

Responses:

| Status | Code                 | Meaning                                              |
| ------ | -------------------- | ---------------------------------------------------- |
| 202    | —                    | Cleared; body `{"ok": true}`                         |
| 403    | `BAD_REQUEST`        | Missing `X-Requested-With: contextmatrix`            |
| 404    | `CHAT_NOT_FOUND`     | Unknown session id                                   |
| 409    | `RUNNER_NOT_RUNNING` | Session is not active or warm-idle (no live runner)  |
| 502    | `RUNNER_UNAVAILABLE` | Runner `/clear` or primer send failed (see `detail`) |
| 500    | `INTERNAL_ERROR`     | Persistence failure (rare; transcript-side)          |

On a `502` the transcript is left untouched — the operator can retry once
the runner is reachable again. On `500` the runner has already been
cleared but the transcript mark/divider step failed; the session is still
usable, the divider just won't appear in the UI until the next clear.

The `502` response body includes a `detail` field that distinguishes the
two failure stages:

| `detail`        | Meaning                                                                        |
| --------------- | ------------------------------------------------------------------------------ |
| `clear_failed`  | The runner `/clear` call failed; primer was never attempted                    |
| `primer_failed` | The `/clear` succeeded but the primer re-send failed; runtime is unoriented    |

Example SSE event for the divider (default unnamed channel, matching the
existing message wire shape):

```json
{
  "seq": 42,
  "role": "system",
  "content": "Context cleared",
  "kind": "divider"
}
```

`rehydration_phase` is omitted when `false` (`omitempty` on the wire), so it
does not appear in a normal Clear Context event. It will be present and `true`
only when Clear is invoked while the session is in its rehydration phase (rare).

### POST /api/chats/{id}/messages

Send a user message into the active chat container. The Manager appends the
message to the transcript with a server-assigned seq, broadcasts a `user` event
on the per-session SSE hub, then forwards the message to the runner.

Request:

```json
{ "content": "Show me the diff between v1 and v2 of the auth middleware." }
```

`content` is capped at 8 KiB (`413` on overflow).

Response (`202 Accepted`):

```json
{ "ok": true, "message_id": "msg-1234abcd" }
```

### GET /api/chats/{id}/messages

Bootstrap endpoint that returns persisted transcript rows from SQLite, ordered
oldest-first. Used by the browser on Chat tab mount (and on refresh) to backfill
the in-memory ring buffer beyond what the SSE in-memory replay (128 entries) can
cover.

Query parameters:

| Param       | Default | Max  | Effect                                    |
| ----------- | ------- | ---- | ----------------------------------------- |
| `since_seq` | `0`     | —    | Exclusive lower bound: returns `seq > N`. |
| `limit`     | `200`   | 1000 | Cap on rows returned. Values above clamp. |

Response:

```json
{
  "messages": [
    {
      "id": 1,
      "session_id": "01J...",
      "seq": 1,
      "role": "user",
      "content": "{\"text\":\"hi\"}",
      "created_at": "2026-05-14T12:00:00Z"
    },
    {
      "id": 2,
      "session_id": "01J...",
      "seq": 2,
      "role": "assistant_text",
      "content": "{\"text\":\"hello\"}",
      "created_at": "2026-05-14T12:00:01Z"
    }
  ]
}
```

Empty transcripts return `{"messages": []}`. Invalid `since_seq` / `limit`
return `400 BAD_REQUEST`. Unknown session returns `404 CHAT_NOT_FOUND`.

The browser pairs this REST bootstrap with the SSE `/stream` endpoint: fetches
all messages with `since_seq=0`, records the highest seq, then subscribes to
`/stream?since_seq=<last>` so the seam is gapless. SSE events whose `seq` falls
inside the REST window are deduped on the client.

### GET /api/chats/{id}/stream

Server-Sent Events stream of new transcript entries for one session. Two event
kinds share the wire:

- **Default (transcript) event** — emitted without an SSE `event:` header so
  older `EventSource.onmessage` listeners keep working. Payload:

  ```json
  {
    "seq": 7,
    "role": "assistant_text",
    "content": "{\"text\":\"…\"}",
    "rehydration_phase": false
  }
  ```

  `rehydration_phase` is omitted when false so the UI can group rehydration
  turns distinctly from normal traffic.

- **`session_updated` event** — emitted with `event: session_updated` so the
  browser can listen on a named channel. The payload is a `SessionUpdate` object
  (zero-valued fields omitted via `omitempty`; merge into your local session view):

  | Field                        | Type      | Description                                                                  |
  |------------------------------|-----------|------------------------------------------------------------------------------|
  | `context_tokens`             | integer   | Updated context-window token count.                                          |
  | `context_tokens_updated_at`  | timestamp | When `context_tokens` was last updated.                                      |
  | `model`                      | string    | Model name, set on first usage event.                                        |
  | `rehydration_active`         | boolean   | `false` once `chat_rehydration_complete` is called.                          |
  | `status`                     | string    | Lifecycle transition. Only present when the status changed — pointer semantics distinguish "no change" from a deliberate value. See `ChatStatus` values below. |
  | `prompt_tokens`              | int64     | New cumulative prompt-token total after this usage frame. Omitted when zero (no cost update in this event). |
  | `completion_tokens`          | int64     | New cumulative completion-token total. Omitted when zero.                    |
  | `cache_read_tokens`          | int64     | New cumulative cache-read-token total. Omitted when zero.                    |
  | `cache_creation_tokens`      | int64     | New cumulative cache-creation-token total. Omitted when zero.                |
  | `estimated_cost_usd`         | float64   | New running cost total in USD after this frame. Omitted when zero. Matches the value persisted on the session row. |

  **`ChatStatus` values:**

  | Value       | Description                                                                                          |
  |-------------|------------------------------------------------------------------------------------------------------|
  | `cold`      | Session is idle; no runner container. Starting state and the state after `EndSession`.               |
  | `active`    | Runner container is running and a browser subscriber is present.                                     |
  | `warm-idle` | Container still running but no browser subscriber (grace window). Reverts to `active` on resubscribe. |
  | `ending`    | Transient teardown state; not emitted as an SSE `status`.                                                 |

  **Lifecycle transitions that emit a `status` field:**
  - `cold → active` via `OpenSession` (POST `/open`)
  - `warm-idle → active` when a browser subscriber attaches (OnSubscribe) or `OpenSession` is called
  - `active → warm-idle` after the 30 s grace timer fires following last-subscriber departure
  - `active/warm-idle → cold` via `EndSession` (POST `/end`) or the idle reaper

  Clients should refetch `GET /api/chats` when `status` changes to update sidebar indicators.

Query parameter: `since_seq=<N>` (replay events where `seq > N` from the
server-side 128-entry ring buffer before tailing live events). The handler
flushes a `: connected\n\n` comment immediately on subscribe so browsers see
`onopen` fire before any event lands, and sends `: keepalive\n\n` every 15
seconds. SSE write deadlines are cleared per-connection so the stream survives
the server-wide `WriteTimeout`. Subscribing to an unknown session returns
`404 CHAT_NOT_FOUND` (the handler validates the session exists before reaching
the hub).

## Image Endpoints

Backs the paste / drag-drop image upload flow in `CardPanelEditor` and the
inline image attachments on the MCP `get_card` / `get_task_context` tools.
Images live in a separate SQLite DB (`images.db`, configurable via
`images.db_path` / `CONTEXTMATRIX_IMAGES_DB_PATH`). IDs are the first 16 hex
chars of `sha256(processed_bytes)` — identical uploads dedup naturally and
URLs are stable.

### POST /api/images

Multipart form upload. Single field `file`. Returns the content-hashed id and
the canonical URL for embedding in markdown via `![](...)`.

Body size cap: 10 MB (the global 5 MB `bodyLimit` is overridden to 11 MB on
this route alone — 1 MB headroom for the multipart envelope).

Server-side processing (`internal/images/processor.go`):

- Accepts `image/png`, `image/jpeg`, `image/gif` (single-frame), `image/webp`.
- Rejects animated GIFs explicitly with `IMAGE_ANIMATED` / 415 via a
  pre-decode header walk (`gifHasMultipleFrames`).
- Animated WebPs reject as `IMAGE_UNSUPPORTED` / 415 because the stdlib
  `webp.Decode` is still-only and returns an invalid-format error on
  animated inputs; there is no dedicated animated-WebP gate.
- Rejects anything else (video, octet-stream, …) with `IMAGE_UNSUPPORTED` / 415.
- Rejects payloads larger than 10 MB with `CONTENT_TOO_LARGE` / 413.
- Resizes to fit within 1024x768 preserving aspect ratio (CatmullRom).
- Re-encodes in the same format (PNG / JPEG q85). Single-frame GIF and WebP
  are re-encoded as PNG since stdlib lacks a WebP encoder. The re-encode
  strips EXIF naturally — no separate parser.

```bash
curl -X POST http://localhost:8080/api/images \
  -H 'X-Requested-With: contextmatrix' \
  -H 'X-Agent-ID: human:alice' \
  -F 'file=@screenshot.png'
```

Response 201:

```json
{ "id": "aabbccddeeff0011", "url": "/api/images/aabbccddeeff0011" }
```

Error codes: `CONTENT_TOO_LARGE` (413), `IMAGE_UNSUPPORTED` / `IMAGE_ANIMATED`
(415), `IMAGE_MISSING_FILE` / `IMAGE_INVALID_PAYLOAD` (400).

### GET /api/images/{id}

Serves the stored blob with the original `Content-Type` and
`Cache-Control: public, max-age=31536000, immutable`. Content-hashed IDs
guarantee bytes never change for a given URL, so aggressive caching is safe.

Returns 404 with `IMAGE_NOT_FOUND` when no row matches.

## MCP Endpoints

The MCP (Model Context Protocol) server is mounted at `/mcp` when an MCP API key
is configured. The same handler is registered for `POST /mcp`, `GET /mcp`, and
`DELETE /mcp` per the MCP Streamable HTTP transport spec. Authentication uses a
`Bearer <api-key>` `Authorization` header. The path is exempt from the CSRF
guard. See [`docs/agent-workflow.md`](agent-workflow.md) for the tool and prompt
catalogue.

### `get_card` / `get_task_context` — inline image attachments

Both tools scan the primary card body for markdown image refs of the form
`![](/api/images/<16-hex>)` (relative or absolute against this server). Matching
image bytes are loaded from the image store and attached to the tool response
as MCP `ImageContent` blocks alongside the JSON `TextContent` block, so agents
can *see* screenshots, not just URL strings.

- Capped at **10 images per call** to bound context.
- Capped at **~20 MiB cumulative raw image bytes per call**. The server walks
  references in order and stops attaching as soon as the next image would
  exceed the budget; remaining refs are kept in the body but not inlined.
  Truncation is logged with the tool name and originating card id so an
  operator can correlate.
- Unknown IDs (e.g. dangling references after migration) are silently skipped.
- Pass `include_images: false` to opt out and get a text-only result.
- `get_task_context` only scans the primary card body — sibling cards stay
  text-only. `list_cards` is unaffected (it does not return full bodies).

### `chat_rehydration_complete`

Marks the active chat session's rehydration phase as complete and emits the
final summary message. Called by a chat-mode worker after it has finished
ingesting the rehydration prompt.

**Identity gate:** the caller's `X-CM-Chat-Session` header must equal the
`session_id` parameter; otherwise the call is rejected. The empty-caller case
(no header) is allowed for card-mode and out-of-band callers, but the session_id
must still resolve to an active chat session.

**Parameters:**

- `session_id` (string, required)
- `summary` (string, required) — surfaced to the UI as an assistant message
