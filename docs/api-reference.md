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

POST   /api/projects/{project}/cards/{id}/run         # trigger remote execution (human-only)
POST   /api/projects/{project}/cards/{id}/stop        # stop running task (human-only)
POST   /api/projects/{project}/stop-all               # stop all running tasks (human-only)
POST   /api/runner/status                              # runner status callback (HMAC-signed)

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

Stop all running remote executions in a project. Human-only.
Returns `{ "affected_cards": ["PROJ-001", "PROJ-003"] }`.

### POST /api/runner/status

Runner callback endpoint. Must include `X-Signature-256` header with
HMAC-SHA256 signature. Accepts `runner_status` updates (`"running"`,
`"failed"`).

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "runner_status": "running",
  "message": "container started"
}
```
