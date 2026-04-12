# REST API Reference

```
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

GET    /api/projects/{project}/usage                  # aggregated token usage
GET    /api/projects/{project}/dashboard              # project dashboard metrics
POST   /api/projects/{project}/recalculate-costs      # recalculate token costs

POST   /api/projects/{project}/cards/{id}/run         # trigger remote execution (human-only)
POST   /api/projects/{project}/cards/{id}/stop        # stop running task (human-only)
POST   /api/projects/{project}/stop-all               # stop all running tasks (human-only)
POST   /api/runner/status                              # runner status callback (HMAC-signed)
GET    /api/runner/logs?project=                      # SSE log stream proxy (runner must be enabled)

POST   /api/sync                                      # trigger git sync
GET    /api/sync                                       # sync status

GET    /api/jira/status                               # check if Jira is configured
GET    /api/jira/epic/{epicKey}                        # preview epic + children (no import)
POST   /api/jira/import-epic                           # create project from epic (human-only)

GET    /api/events?project=                           # SSE stream
GET    /healthz                                        # health check
```

**Agent identification:** `X-Agent-ID` header on all requests. For mutations on
claimed cards, the header value must match `assigned_agent` — otherwise 403.

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
- 429: Jira rate limit exceeded (`JIRA_RATE_LIMITED`)
- 502: runner webhook failed (bad gateway), or Jira authentication failed (`JIRA_UNAUTHORIZED`)
- 503: runner not configured, or Jira not configured (`JIRA_NOT_CONFIGURED`)

**Error codes relevant to vetting:**

| Code                | HTTP | When                                                                                       |
| ------------------- | ---- | ------------------------------------------------------------------------------------------ |
| `CARD_NOT_VETTED`   | 403  | A non-human agent calls `POST /claim` on a card with `source != null && vetted == false`. |
| `HUMAN_ONLY_FIELD`  | 403  | An agent without `human:` prefix attempts to set `vetted`, `autonomous`, `feature_branch`, or `create_pr`. |

### Card list query parameters

| Parameter     | Values          | Description                                                                                     |
| ------------- | --------------- | ----------------------------------------------------------------------------------------------- |
| `state`       | state name      | Filter by card state                                                                            |
| `type`        | type name       | Filter by card type                                                                             |
| `label`       | label string    | Filter cards that have this label                                                               |
| `agent`       | agent ID        | Filter by `assigned_agent`                                                                      |
| `parent`      | card ID         | Filter by parent card                                                                           |
| `priority`    | priority name   | Filter by priority                                                                              |
| `external_id` | external ID     | Filter by `source.external_id` (idempotent import check)                                       |
| `vetted`      | `true` / `false`| Filter by `vetted` field. `?vetted=false` lists unvetted external cards awaiting human review. |

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

Trigger a git pull on the boards repository. Returns 503 if sync is disabled
(no remote configured).

### GET /api/sync

Returns current sync status.

```json
{ "enabled": true, "last_sync": "2026-04-05T12:00:00Z", "error": "" }
```

## Runner Endpoints

See [`docs/remote-execution.md`](remote-execution.md) for the full webhook
protocol, HMAC signing details, and runner configuration.

### POST /api/projects/{project}/cards/{id}/run

Trigger remote execution for an autonomous card. Human-only (rejects
`X-Agent-ID` without `human:` prefix). Requires card to be in `todo` state with
`autonomous: true` and runner enabled globally + per-project.

Returns the updated card with `runner_status: "queued"`.

### POST /api/projects/{project}/cards/{id}/stop

Stop a running remote execution. Human-only. Sends kill webhook to runner.
Returns the updated card with `runner_status: "killed"`.

### POST /api/projects/{project}/stop-all

Stop all running remote executions in a project. Human-only. Returns
`{ "affected_cards": ["PROJ-001", "PROJ-003"] }`.

### GET /api/runner/logs

SSE proxy that streams live log entries from the runner. Only available when
runner is enabled (`runner.enabled: true` in config). Not authenticated — the
browser connects directly; HMAC signing is performed server-side toward the
runner.

**Query parameter:** `?project=<name>` — filters entries to a single project.
Omit to receive all projects.

**Response:** `Content-Type: text/event-stream`. Each event carries a JSON
`LogEntry`:

```json
{
  "ts": "2026-04-08T12:34:56.789Z",
  "card_id": "PROJ-042",
  "project": "my-project",
  "type": "text",
  "content": "Planning the implementation..."
}
```

`type` is one of: `text`, `thinking`, `tool_call`, `stderr`, `system`.

Keepalive comments (`: keepalive`) are forwarded every 15 seconds. The
connection is closed when the browser disconnects, which in turn cancels the
upstream request to the runner.

See [`docs/remote-execution.md`](remote-execution.md) for the full log
streaming architecture and `LogEntry` type details.

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

---

## Jira integration

### GET /api/jira/status

Returns whether Jira integration is configured. No credentials exposed.

```json
{ "configured": true, "base_url": "https://company.atlassian.net" }
```

### GET /api/jira/epic/{epicKey}

Preview a Jira epic and its child issues without importing. Requires Jira to be
configured.

**Response (200):**

```json
{
  "epic": {
    "key": "PROJ-42",
    "summary": "Auth overhaul",
    "status": "In Progress",
    "issue_type": "Epic"
  },
  "children": [
    {
      "key": "PROJ-43",
      "summary": "Add OAuth support",
      "status": "To Do",
      "issue_type": "Story"
    }
  ]
}
```

**Error codes:** `JIRA_NOT_FOUND` (404), `JIRA_UNAUTHORIZED` (502),
`JIRA_RATE_LIMITED` (429), `JIRA_NOT_CONFIGURED` (503).

### POST /api/jira/import-epic

Create a CM project from a Jira epic with all child issues as cards. **Human-only**
— requests with `X-Agent-ID` header are rejected with 403.

**Request:**

```json
{
  "epic_key": "PROJ-42",
  "name": "auth-overhaul",
  "prefix": "PROJ"
}
```

`name` and `prefix` are optional — derived from the epic summary and Jira project
key if omitted.

**Response (201):**

```json
{
  "project": { "name": "auth-overhaul", "prefix": "PROJ", "..." : "..." },
  "cards_imported": 12,
  "skipped": 0
}
```

**Error codes:**

| Code                 | HTTP | When                                                   |
| -------------------- | ---- | ------------------------------------------------------ |
| `BAD_REQUEST`        | 400  | Missing or invalid `epic_key`                          |
| `HUMAN_ONLY_FIELD`   | 403  | Request includes `X-Agent-ID` header (human-only)      |
| `JIRA_NOT_FOUND`     | 404  | Epic not found in Jira                                 |
| `JIRA_RATE_LIMITED`  | 429  | Jira rate limit exceeded                               |
| `JIRA_UNAUTHORIZED`  | 502  | Jira authentication failed                             |
| `JIRA_NOT_CONFIGURED`| 503  | Jira integration not configured                        |

**Notes on partial imports:**

- `skipped`: number of child issues not imported because they already exist (by
  `source.external_id`) or failed to create.
- If the import is interrupted (network failure, timeout), the project and
  already-created cards persist — there is no rollback.

Imported cards use the Jira issue key as the card ID (e.g., `PROJ-43`). The
board prefix is derived from the Jira project key (e.g., `PROJ`, not `PROJ42`).
Imported cards have `source.system: "jira"`, `source.external_id` set to the
Jira issue key, and `vetted: true` (human-initiated import is considered vetted).
