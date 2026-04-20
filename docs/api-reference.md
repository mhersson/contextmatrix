# REST API Reference

```text
GET    /api/projects
POST   /api/projects                                     # create project
GET    /api/projects/{project}
PUT    /api/projects/{project}                            # update project config
DELETE /api/projects/{project}                            # delete project (requires 0 cards)

GET    /api/projects/{project}/cards            ?state=&type=&label=&agent=&parent=&priority=&external_id=&vetted=
POST   /api/projects/{project}/cards
GET    /api/projects/{project}/cards/{id}
PUT    /api/projects/{project}/cards/{id}
PATCH  /api/projects/{project}/cards/{id}
DELETE /api/projects/{project}/cards/{id}

POST   /api/projects/{project}/cards/{id}/claim      { "agent_id": "..." }
POST   /api/projects/{project}/cards/{id}/release     { "agent_id": "..." }
POST   /api/projects/{project}/cards/{id}/heartbeat   { "agent_id": "..." }
POST   /api/projects/{project}/cards/{id}/log         { "agent_id": "...", "action": "...", "message": "..." }

GET    /api/projects/{project}/cards/{id}/context
POST   /api/projects/{project}/cards/{id}/usage       # report token usage
POST   /api/projects/{project}/cards/{id}/report-push # record git push / PR

GET    /api/projects/{project}/branches               # list branches from project's GitHub repo
GET    /api/projects/{project}/usage                  # aggregated token usage
GET    /api/projects/{project}/dashboard              # project dashboard metrics
POST   /api/projects/{project}/recalculate-costs      # recalculate token costs

POST   /api/projects/{project}/cards/{id}/run         # trigger remote execution (human-only)
POST   /api/projects/{project}/cards/{id}/stop        # stop running task (human-only)
POST   /api/projects/{project}/cards/{id}/message     # send chat message to running container (human-only)
POST   /api/projects/{project}/cards/{id}/promote     # promote interactive session to autonomous (human-only)
POST   /api/projects/{project}/stop-all               # stop all running tasks (human-only)
POST   /api/runner/status                              # runner status callback (HMAC-signed)
GET    /api/runner/logs?project=&card_id=              # SSE log stream (card-scoped or project-scoped; runner must be enabled)

POST   /api/sync                                      # trigger git sync
GET    /api/sync                                       # sync status

GET    /api/app/config                                 # server-side app config (theme/palette)

GET    /api/events?project=                           # SSE stream
GET    /healthz                                        # liveness probe (shallow)
GET    /readyz                                         # readiness probe (dependency-checked)
```

**Admin/debug server:** when `admin_port` is configured (non-zero), a separate
HTTP server binds to `admin_bind_addr` (default `127.0.0.1`) and serves:

- `GET /metrics` — Prometheus text exposition format.
- `GET /debug/pprof/*` — Go runtime profiling (heap, goroutine, profile, etc.).

Neither endpoint is exposed on the main listener. The admin listener has no
built-in authentication — keep it loopback-only, or gate it with a firewall /
NetworkPolicy / service-mesh rule.

**Agent identification:** `X-Agent-ID` header on all requests. For mutations on
claimed cards, the header value must match `assigned_agent` — otherwise 403.

**Request correlation:** every response carries an `X-Request-ID` header. If
the client sends an `X-Request-ID` matching `[A-Za-z0-9._-]{1,128}` it is
echoed; otherwise the server generates a UUID. The same id is emitted as the
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

- 200: success (GET, PUT, PATCH)
- 201: created (POST)
- 204: deleted (DELETE)
- 403: agent mismatch (wrong agent trying to modify claimed card), unvetted card
  claim attempt (`CARD_NOT_VETTED`), or agent attempting a human-only field
  mutation (`HUMAN_ONLY_FIELD`)
- 404: card or project not found
- 409: conflict (invalid transition, card already claimed)
- 422: validation error (missing required fields, unknown type/state/priority)
- 502: runner webhook failed (bad gateway)
- 503: runner not configured

**Error codes relevant to vetting:**

| Code               | HTTP | When                                                                                                                      |
| ------------------ | ---- | ------------------------------------------------------------------------------------------------------------------------- |
| `CARD_NOT_VETTED`  | 403  | A non-human agent calls `POST /claim` on a card with `source != null && vetted == false`.                                 |
| `HUMAN_ONLY_FIELD` | 403  | An agent without `human:` prefix attempts to set `vetted`, `autonomous`, `feature_branch`, `create_pr`, or `base_branch`. |

## Health Endpoints

### GET /healthz

Shallow liveness probe. Always returns `200 OK` with the text body `ok` as long
as the process is running. No dependency checks are performed.

Use this as a k8s `livenessProbe` target (or equivalent). Do not use it to gate
traffic — a `200` from `/healthz` only means the process has not crashed.

```bash
curl http://localhost:8080/healthz
# → ok
```

### GET /readyz

Dependency-checked readiness probe. Runs three checks with a 500 ms timeout:

| Check         | What it tests                                         |
| ------------- | ----------------------------------------------------- |
| `store`       | `ListProjects` succeeds (boards directory is readable) |
| `git`         | `CurrentBranch` resolves (git manager is initialised) |
| `session_log` | session-log manager is not nil (runner is operational) |

Returns **200** when all checks pass, **503** when any check fails.

**Response body (200):**

```json
{
  "status": "ok",
  "checks": [
    { "name": "store",       "ok": true },
    { "name": "git",         "ok": true },
    { "name": "session_log", "ok": true }
  ]
}
```

**Response body (503):**

```json
{
  "status": "degraded",
  "checks": [
    { "name": "store", "ok": false, "error": "open /data/boards: permission denied" },
    { "name": "git",         "ok": true },
    { "name": "session_log", "ok": true }
  ]
}
```

Use this as a k8s `readinessProbe` target. Kubernetes operators should point:

- `readinessProbe` → `GET /readyz`
- `livenessProbe`  → `GET /healthz`

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

## App Endpoints

### GET /api/app/config

Returns the server-configured application settings. Unauthenticated — safe for
public read. Called by the frontend on startup to determine which color palette
to apply.

**Response:**

```json
{ "theme": "everforest" }
```

`theme` is one of `"everforest"` (default), `"radix"`, or `"catppuccin"`. The
frontend sets `data-palette` on `<html>` to match the theme value;
`"everforest"` removes the attribute (it is the default CSS block).

```bash
curl http://localhost:8080/api/app/config
# → {"theme":"everforest"}
```

## Agent Endpoints

### POST /api/projects/{project}/cards/{id}/usage

Report token usage for a card. Accumulates across multiple calls.

```json
{
  "agent_id": "claude-7a3f",
  "model": "claude-sonnet-4-6",
  "prompt_tokens": 1234,
  "completion_tokens": 567
}
```

Returns 200 with the updated card. Cost is calculated automatically from
`token_costs` in `config.yaml` if the model matches a configured key.

### POST /api/projects/{project}/cards/{id}/report-push

Record a git push and optional PR URL on a card. Branch protection is enforced —
pushing to `main` or `master` returns 403 `PROTECTED_BRANCH`.

```json
{
  "agent_id": "claude-7a3f",
  "branch": "feat/user-auth",
  "pr_url": "https://github.com/org/repo/pull/42"
}
```

Returns 200 with the updated card.

## Project Endpoints

### GET /api/projects/{project}/branches

Returns a JSON array of branch name strings for the project's GitHub repository.
Used by the card editor to populate the base branch dropdown.

Requires a GitHub token to be configured (`github.token` in `config.yaml` or
`CONTEXTMATRIX_GITHUB_TOKEN`). Returns 503 if no token is configured. Returns
404 with `NO_GITHUB_REPO` if the project's `repo` field is not a GitHub URL.

```json
["main", "develop", "release/v2", "feat/some-branch"]
```

**Error codes:**

| Code              | HTTP | When                                          |
| ----------------- | ---- | --------------------------------------------- |
| `NO_GITHUB_TOKEN` | 503  | `github.token` is not configured              |
| `NO_GITHUB_REPO`  | 404  | Project `repo` is not a GitHub repository URL |

### GET /api/projects/{project}/usage

Returns aggregated token usage across all cards in a project.

```json
{
  "prompt_tokens": 45000,
  "completion_tokens": 12000,
  "estimated_cost_usd": 0.315,
  "card_count": 8
}
```

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
  "cards_completed_today": 2,
  "agent_costs": [
    {
      "agent_id": "claude-7a3f",
      "prompt_tokens": 30000,
      "completion_tokens": 8000,
      "estimated_cost_usd": 0.21,
      "card_count": 5
    }
  ],
  "card_costs": [
    {
      "card_id": "ALPHA-003",
      "card_title": "...",
      "prompt_tokens": 5000,
      "completion_tokens": 1200,
      "estimated_cost_usd": 0.033
    }
  ]
}
```

### POST /api/projects/{project}/recalculate-costs

Recalculate estimated costs for all cards in a project using current
`token_costs` rates. Requires `default_model` for cards that have tokens but no
model recorded.

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
{ "enabled": true, "last_sync": "2026-04-05T12:00:00Z", "error": "" }
```

## Runner Endpoints

See [`docs/remote-execution.md`](remote-execution.md) for the full webhook
protocol, HMAC signing details, and runner configuration.

### POST /api/projects/{project}/cards/{id}/run

Trigger remote execution for a card. Human-only (rejects `X-Agent-ID` without
`human:` prefix). Requires card to be in `todo` state and runner enabled
globally + per-project. The `autonomous` flag is **not** required.

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

Returns the updated card with `runner_status: "queued"`.

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

1. Calls `CardService.PromoteToAutonomous` to flip the card's `autonomous`
   flag to `true`, append an activity log entry (`action=promoted`), commit the
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
- 502 `RUNNER_ERROR` if the runner webhook fails — the autonomous flag flip is
  **not** reverted; the card is permanently promoted

Returns 200 with the updated card.

```bash
curl -X POST http://localhost:8080/api/projects/my-project/cards/PROJ-042/promote \
  -H "X-Agent-ID: human:alice"
```

### POST /api/projects/{project}/cards/{id}/stop

Stop a running remote execution. Human-only. Sends kill webhook to runner.
Returns the updated card with `runner_status: "killed"`.

### POST /api/projects/{project}/stop-all

Stop all running remote executions in a project. Human-only. Returns
`{ "affected_cards": ["PROJ-001", "PROJ-003"] }`.

### GET /api/runner/logs

SSE log stream. Only available when runner is enabled (`runner.enabled: true` in
config). Not authenticated — the browser connects directly; HMAC signing is
performed server-side toward the runner.

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
`X-Accel-Buffering: no` on all SSE responses to bypass nginx proxy buffering.
A `: keepalive\n\n` comment is written every 30 seconds per subscription to
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

| Frame type | Payload shape | Meaning |
| ---------- | ------------- | ------- |
| `terminal` | `{"type":"terminal","seq":N}` | Session ended; no further events |
| `dropped` | `{"type":"dropped","seq":N,"count":N}` | Server ring-buffer overflowed; `count` events were evicted |

`type` for normal events is one of: `text`, `thinking`, `tool_call`, `stderr`,
`system`, `user`.

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

Runner callback endpoint. Must include `X-Signature-256` header with HMAC-SHA256
signature. Accepts `runner_status` updates (`"running"`, `"failed"`).

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "runner_status": "running",
  "message": "container started"
}
```
