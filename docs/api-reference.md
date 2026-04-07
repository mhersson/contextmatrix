# REST API Reference

```
GET    /api/projects
POST   /api/projects                                     # create project
GET    /api/projects/{project}
PUT    /api/projects/{project}                            # update project config
DELETE /api/projects/{project}                            # delete project (requires 0 cards)

GET    /api/projects/{project}/cards            ?state=&type=&label=&agent=&parent=&priority=&external_id=
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

POST   /api/sync                                      # trigger git sync
GET    /api/sync                                       # sync status

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
- 403: agent mismatch (wrong agent trying to modify claimed card)
- 404: card or project not found
- 409: conflict (invalid transition, card already claimed)
- 422: validation error (missing required fields, unknown type/state/priority)
- 502: runner webhook failed (bad gateway)
- 503: runner not configured

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
