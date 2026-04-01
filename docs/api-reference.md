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
