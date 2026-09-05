# REST API Reference

Every route the server registers, grouped by family. Related docs:
[MCP integration](mcp.md) for the tool catalogue, [remote
execution](remote-execution.md) for the webhook protocol,
[authentication](authentication.md) for the multi-user setup, [data
model](data-model.md) for card and project field semantics.

```text
GET    /api/projects
POST   /api/projects                                     # create project (admin-only in multi mode)
GET    /api/projects/{project}
PUT    /api/projects/{project}                            # update project config (admin-only in multi mode)
DELETE /api/projects/{project}                            # delete project (requires 0 cards; admin-only in multi mode)

GET    /api/projects/{project}/cards            ?state=&type=&label=&agent=&parent=&priority=&external_id=&vetted=&limit=&cursor=
POST   /api/projects/{project}/cards
GET    /api/projects/{project}/cards/{id}
PUT    /api/projects/{project}/cards/{id}
PATCH  /api/projects/{project}/cards/{id}
DELETE /api/projects/{project}/cards/{id}

POST   /api/projects/{project}/cards/{id}/claim          # identity from the session (multi mode) or X-Agent-ID
POST   /api/projects/{project}/cards/{id}/release        # same identity rule
POST   /api/projects/{project}/cards/{id}/force-release  # human-only; clears another agent's claim
# heartbeat, log, context, usage, and report-push have no REST endpoint - the
# MCP tools (`heartbeat`, `add_log`, `get_task_context`, `report_usage`,
# `report_push`) are the only interface; agents never call REST directly.

GET    /api/projects/{project}/branches               # list branches from project's GitHub repo
GET    /api/projects/{project}/usage                  # aggregated token usage
GET    /api/projects/{project}/dashboard              # project dashboard metrics
GET    /api/projects/{project}/activity   ?limit=     # flattened activity-log feed (newest first; cap 500)
POST   /api/projects/{project}/recalculate-costs      # recalculate token costs (admin-only in multi mode)

POST   /api/projects/{project}/cards/{id}/run         # trigger remote execution (human-only)
POST   /api/projects/{project}/cards/{id}/stop        # stop running task (human-only)
POST   /api/projects/{project}/cards/{id}/message     # send chat message to running container (human-only)
POST   /api/projects/{project}/cards/{id}/promote     # promote interactive session to autonomous (human-only)
POST   /api/projects/{project}/stop-all               # stop all running tasks (human-only)
POST   /api/agent/status                               # task-backend worker-status callback (HMAC-signed; task-backend required)
GET    /api/agent/task-skills-source                   # task-skills {git_remote_url, ref} pointer (HMAC-signed; task-backend required)
GET    /api/chat/task-skills-source                    # same shape for the chat backend (HMAC-signed; chat-backend required)
GET    /api/agent/git-credentials  ?project=&card_id=  # mid-run project git-token refresh (HMAC-signed; task-backend required)
GET    /api/backend/health                             # proxied backend /health (capacity meter; 2s cached; fixed path)
GET    /api/backends/{backend}/images                  # proxied backend /images (worker-image picker; agent|chat; 30s cached; admin-gated in multi mode)
GET    /api/worker/logs  ?project=&card_id=            # SSE log stream (card-scoped or project-scoped; fixed path; task-backend required)
GET    /api/v1/cards/{project}/{id}/autonomous         # backend autonomous-flag read (HMAC-signed; task-backend required)
# /api/agent/* - task-backend callback path; /api/chat/* - chat-backend callback path (paths are fixed)

GET    /api/worker/git-credentials  ?host=&path=       # per-repo git credentials for chat workers (bearer-authed, not HMAC; chat backend + manager required)

GET    /api/chats                                      ?project=&status=&created_by=&limit=
POST   /api/chats                                      # create a new chat session (cold)
GET    /api/chats/models                               # chat model picker source (openrouter|endpoint)
GET    /api/chats/{id}
PATCH  /api/chats/{id}                                 # rename a session
DELETE /api/chats/{id}                                 # delete session and transcript
POST   /api/chats/{id}/open                            # start (or reattach to) the chat container
POST   /api/chats/{id}/end                             # stop the container; flip to cold
POST   /api/chats/{id}/clear                           # clear worker context + mark transcript
POST   /api/chats/{id}/messages                        # send a user message into the active container
GET    /api/chats/{id}/messages                        ?since_seq=|tail=1|before_seq=&limit=  # transcript pages
GET    /api/chats/{id}/stream                          ?since_seq=           # SSE stream of new entries

POST   /api/sync                                       ?repo=                # trigger git sync
GET    /api/sync                                       # sync status per boards repo

GET    /api/playbooks                                  # list playbook summaries (optional subsystem; see below)
POST   /api/playbooks                                  # create playbook (title, description?, entries?)
GET    /api/playbooks/{id}                             # resolved detail
PATCH  /api/playbooks/{id}                             # update title/description
DELETE /api/playbooks/{id}                             # delete playbook
POST   /api/playbooks/{id}/entries                     # append entry (card reference or manual step)
PATCH  /api/playbooks/{id}/entries/{entryId}            # update entry (done/note/text/position)
DELETE /api/playbooks/{id}/entries/{entryId}            # remove entry

GET    /api/task-skills                                # list available task skill names
GET    /api/app/config                                 # server-side app config (slim pre-login payload in multi mode)
GET    /api/models                                     # model catalog for the card model-pin pickers (source: openrouter|endpoint|none)

POST   /api/images                                     # multipart image upload (content-hashed id; 10 MB cap)
GET    /api/images/{id}                                # serve stored image blob (immutable cache headers)

POST   /api/auth/login                                  # session-cookie login (multi mode only)
POST   /api/auth/logout                                 # clear session (multi mode only)
GET    /api/auth/session                                # who am I (multi mode only; 401 if not logged in)
GET    /api/auth/token/{token}                          # inspect a bootstrap/invite/reset token (multi mode only)
POST   /api/auth/token/{token}                          # redeem token: set password + auto-login (multi mode only)
POST   /api/auth/password                                # change own password (multi mode only)
GET    /api/users                                        # user roster for pickers (any session, multi mode only)

GET    /api/admin/users                                 # list accounts (admin-only, multi mode only)
POST   /api/admin/users                                 # create account + invite link (admin-only, multi mode only)
PATCH  /api/admin/users/{username}                       # update display_name/is_admin/disabled (admin-only, multi mode only)
POST   /api/admin/users/{username}/invite                 # regenerate invite/reset link (admin-only, multi mode only)
GET    /api/admin/credentials                            # list GitHub credential pool metadata (admin-only, multi mode only)
POST   /api/admin/credentials                            # add a pool credential (admin-only, multi mode only)
PUT    /api/admin/credentials/{name}                      # rotate secret / update metadata / disable (admin-only, multi mode only)
DELETE /api/admin/credentials/{name}                      # remove a pool credential (admin-only, multi mode only)

GET    /api/admin/chats                                  # list all chat sessions, metadata only (admin-only, multi mode only; chat manager required)
POST   /api/admin/chats/{id}/end                          # force-end any session (admin-only, multi mode only; chat manager required)
DELETE /api/admin/chats/{id}                              # delete any session (admin-only, multi mode only; chat manager required)

GET    /api/admin/model-outcomes                            # Best-of-N per-model outcome stats (both auth modes; admin-gated only in multi)
DELETE /api/admin/model-outcomes                            # reset recorded outcomes (both auth modes; admin-gated only in multi)
GET    /api/admin/model-blacklist                           # blacklisted models with reasons (both auth modes; admin-gated only in multi)
DELETE /api/admin/model-blacklist/{slug...}                 # delist one model (both auth modes; admin-gated only in multi)

GET    /api/events                                     ?project=             # SSE stream of board events
GET    /healthz                                        # liveness probe (shallow)
GET    /readyz                                         # readiness probe (dependency-checked)

POST   /mcp                                            # MCP Streamable HTTP (Bearer auth when mcp_api_key is set)
GET    /mcp                                            # MCP Streamable HTTP SSE channel
DELETE /mcp                                            # MCP Streamable HTTP session close
```

**Admin/debug server:** when `admin_port` is configured (non-zero), a separate
HTTP server binds to `admin_bind_addr` (default `127.0.0.1`) and serves:

- `GET /metrics` - Prometheus text exposition format.
- `GET /debug/pprof/*` - Go runtime profiling (heap, goroutine, profile, etc.).

Neither endpoint is exposed on the main listener. The admin listener has no
built-in authentication - keep it loopback-only, or gate it with a firewall,
NetworkPolicy, or service-mesh rule.

**Body size:** every request body is capped at 5 MB (413 `CONTENT_TOO_LARGE`);
`POST /api/images` raises its own cap to 11 MB.

**Agent identification:** the `X-Agent-ID` header supplies agent identity. It
is required on `/claim` and `/release` (400 without it) and on any mutation of
a claimed card, where the value must match `assigned_agent` (403
`AGENT_MISMATCH`). It also gates human-only fields and human-only endpoints
(`/run`, `/stop`, `/message`, `/promote`, `/force-release`, `/stop-all`): those
require a value beginning with `human:`. Read endpoints, project CRUD, sync,
branches, app config, task-skills, healthz, and readyz do not require the
header. Request bodies never carry an `agent_id` field; one is silently
ignored if present.

In `auth.mode: multi`, a logged-in session's identity (`human:<username>`)
takes precedence over `X-Agent-ID` on any request that carries a valid session
cookie; the header is consulted only when there is no session (MCP, HMAC
backend callbacks, or `auth.mode: none`). This upgrades the claim/release
ownership check from a courtesy into real enforcement - see §
Authentication (multi mode).

`PATCH /api/projects/{project}/cards/{id}` derives its commit-author identity,
activity-log entry, and SSE event `agent` from this same resolved identity, so
a UI-driven patch commits as `[agent:human:alice] CARD-001: ...` rather than
the generic `[contextmatrix]` marker - see domain rule 5 in
[data-model.md](data-model.md).

**Identity is a tag, not auth (`auth.mode: none`).** In `none` mode
ContextMatrix is single-tenant with no auth layer below `X-Agent-ID`; spoofing
it accomplishes nothing because there is no permission gradient to escalate
into. The `human:` prefix gates workflow contracts (only humans promote), not
security boundaries - true in both modes, since MCP has no session concept.
The web UI generates a per-browser identity (`human:web-<8 hex chars>`) and
never prompts the operator for a username. Routes that act on behalf of the
web UI fall back to `human:web` or `human:api` when no header is present. In
`multi` mode these fallbacks are unreachable on session-gated routes: the
session guard has already rejected any request with no session. See § Trust
model in `AGENTS.md` and in [architecture.md](architecture.md).

**CSRF protection:** every state-changing request on the main listener must
carry `X-Requested-With: contextmatrix`. The web UI sets this header on every
non-GET fetch in `web/src/api/client.ts`. Cross-origin browsers cannot set
custom headers on a "simple request" without a CORS preflight, and the server
serves no permissive CORS for state-changing routes, so a missing header is
rejected with 403 `BAD_REQUEST` before any handler runs. Exempt:

- `GET` / `HEAD` / `OPTIONS` on any route (read-only).
- `/api/agent/*`, `/api/chat/*` - backend callback paths, authenticated via
  per-backend HMAC; not browser paths.
- `/mcp` - Bearer-authed MCP endpoint.
- `/healthz`, `/readyz` - probe endpoints.

Everything else, including `/api/auth/*`, `/api/admin/*`, and `/api/chats/*`,
is guarded. A new state-changing route opts in by not being on this list.

**Request correlation:** every response carries an `X-Request-ID` header. A
client-supplied `X-Request-ID` matching `[A-Za-z0-9._-]{1,128}` is echoed;
otherwise the server generates a UUID. The same id is the `request_id`
attribute on every structured log line the request produces.

**Error response format:**

```json
{
  "error": "invalid state transition",
  "code": "INVALID_TRANSITION",
  "details": "cannot transition from 'todo' to 'done'; valid targets: [in_progress]"
}
```

`details` is omitted when empty. Downstream error strings that look like
go-git transport errors, ssh/exec failures, or absolute filesystem paths are
replaced with stable labels (`"git remote unreachable"`, `"git operation
failed"`, `"filesystem error"`) before they reach a client; JSON decoder
errors become `"invalid JSON at offset N"`, `"invalid type for field ..."`,
or `"request body is empty"`. The raw error is always logged server-side with
the request's `request_id`.

**Response codes:**

| Status | When                                                                                                                                                                                                                                                                                             |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 200    | GET, PUT, PATCH; `POST /claim`, `/release`, `/force-release`, `/stop-all`, `/api/agent/status`, `/api/chats/{id}/open`, `/api/chats/{id}/end`; `DELETE /api/admin/model-outcomes` and `.../model-blacklist/{slug}` (body reports what was deleted); playbook entry DELETE (returns the playbook) |
| 201    | `POST /api/projects`, `POST .../cards`, `POST /api/playbooks`, `POST /api/playbooks/{id}/entries`, `POST /api/chats`, `POST /api/admin/users`, `POST /api/admin/credentials`, `POST /api/images`                                                                                                 |
| 202    | Async work kicked off: `POST .../run`, `/stop`, `/message`, `/promote`, `POST /api/chats/{id}/messages`, `/api/chats/{id}/clear`                                                                                                                                                                 |
| 204    | DELETE of a card, project, playbook, chat, admin chat, or credential; `POST /api/auth/logout`, `POST /api/auth/password`; `GET /api/worker/logs` when no session manager is wired                                                                                                                |
| 207    | `POST .../stop-all` when some cards stopped and others failed to update                                                                                                                                                                                                                          |
| 400    | Malformed input: bad JSON, missing or bad path/query param, unknown filter value, missing CSRF header (`BAD_REQUEST`); unknown task-skill name (`VALIDATION_ERROR`)                                                                                                                              |
| 401    | No or expired session in multi mode (`UNAUTHORIZED`); bad bearer on `/mcp` or `GET /api/worker/git-credentials` in either mode                                                                                                                                                                   |
| 403    | `AGENT_MISMATCH`, `HUMAN_ONLY_FIELD`, `CARD_NOT_VETTED`, `PROTECTED_BRANCH`, `INVALID_SIGNATURE`, `FORBIDDEN` (non-admin on an admin route), or the CSRF guard (`BAD_REQUEST`)                                                                                                                   |
| 404    | Unknown project, card, parent, playbook, entry, chat, image, user, one-time token, credential, backend name, or model slug                                                                                                                                                                       |
| 409    | Conflicts: invalid transition, claim state, dependencies, duplicates, running worker, terminal card, last active admin, bound credential, cold chat session                                                                                                                                      |
| 410    | One-time token already redeemed or expired (`TOKEN_INVALID`)                                                                                                                                                                                                                                     |
| 413    | Request body, chat message, or image over its cap (`CONTENT_TOO_LARGE`)                                                                                                                                                                                                                          |
| 415    | Unsupported or animated image (`IMAGE_UNSUPPORTED`, `IMAGE_ANIMATED`)                                                                                                                                                                                                                            |
| 422    | Mutation body semantically invalid: unknown type/state/priority/phase, bad model pin, field too long, invalid verify or project config, short password, invalid username (`VALIDATION_ERROR`)                                                                                                    |
| 429    | `TOO_MANY_CHATS`, `TOO_MANY_SUBSCRIBERS`, `RATE_LIMITED` (with `Retry-After`)                                                                                                                                                                                                                    |
| 500    | `INTERNAL_ERROR`, `SYNC_ERROR`                                                                                                                                                                                                                                                                   |
| 502    | Backend webhook or probe failed (`BACKEND_UNAVAILABLE`); git-token mint failed on a credentials endpoint (`INTERNAL_ERROR`)                                                                                                                                                                      |
| 503    | `BACKEND_DISABLED`, `SYNC_DISABLED`, `LOGIN_BUSY` (with `Retry-After: 1`), `REMOTE_UNREACHABLE` (shared boards), `/readyz` check failed                                                                                                                                                          |

**Error code / HTTP status mapping:**

| Code                       | HTTP    | Meaning                                                                                                                                                                         |
| -------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BAD_REQUEST`              | 400/403 | Malformed input or unknown filter value (400); missing CSRF header (403)                                                                                                        |
| `VALIDATION_ERROR`         | 422     | Mutation body semantically invalid. Also 400 for an unknown task-skill name, 409 for "last active admin" and "credential bound to projects", 404 for an unknown credential name |
| `UNAUTHORIZED`             | 401     | No or expired session (multi mode); bad bearer on `GET /api/worker/git-credentials`                                                                                             |
| `FORBIDDEN`                | 403     | Authenticated but not admin, on an admin-gated route (multi mode)                                                                                                               |
| `RATE_LIMITED`             | 429     | Too many failed logins for one account+IP; `Retry-After` header set                                                                                                             |
| `LOGIN_BUSY`               | 503     | argon2id concurrency gate saturated; `Retry-After: 1` header set                                                                                                                |
| `TOKEN_INVALID`            | 404/410 | One-time token unknown (404), or already redeemed/expired (410)                                                                                                                 |
| `USER_NOT_FOUND`           | 404     | Unknown username on an admin user-management route                                                                                                                              |
| `PROJECT_NOT_FOUND`        | 404     | Project slug does not exist                                                                                                                                                     |
| `PROJECT_EXISTS`           | 409     | Project slug already exists                                                                                                                                                     |
| `PROJECT_HAS_CARDS`        | 409     | `DELETE /api/projects/{project}` on a project that still has cards                                                                                                              |
| `CARD_NOT_FOUND`           | 404     | Card ID does not exist in the project                                                                                                                                           |
| `CARD_EXISTS`              | 409     | Card ID collision on create                                                                                                                                                     |
| `PARENT_NOT_FOUND`         | 404     | Referenced parent card does not exist                                                                                                                                           |
| `INVALID_TRANSITION`       | 409     | State transition not allowed by the project config; also a claim or promote on a terminal card                                                                                  |
| `DEPENDENCIES_NOT_MET`     | 409     | `depends_on` references an unknown, cross-project, or self card, or would form a cycle                                                                                          |
| `ALREADY_CLAIMED`          | 409     | Card is claimed by another agent (on a shared board the details name the holding instance)                                                                                      |
| `NOT_CLAIMED`              | 409     | Release or force-release on a card with no claim                                                                                                                                |
| `AGENT_MISMATCH`           | 403     | Caller does not own the claim on the card                                                                                                                                       |
| `CARD_NOT_VETTED`          | 403     | Non-human claim on an external card with `vetted: false`                                                                                                                        |
| `HUMAN_ONLY_FIELD`         | 403     | Non-human caller on a human-only field or endpoint                                                                                                                              |
| `PROTECTED_BRANCH`         | 403     | MCP `report_push` targeted `main` / `master`                                                                                                                                    |
| `REVIEW_ATTEMPTS_CAPPED`   | 409     | Review attempts limit reached                                                                                                                                                   |
| `CHAT_NOT_FOUND`           | 404     | Chat session ID does not exist (or belongs to another user in multi mode)                                                                                                       |
| `INVALID_MODEL`            | 400     | Chat `model` not in the active model source                                                                                                                                     |
| `TOO_MANY_CHATS`           | 429     | `chat.max_concurrent` reached, or 32 subscribers already on one chat stream                                                                                                     |
| `TOO_MANY_SUBSCRIBERS`     | 429     | 128 concurrent `GET /api/events` subscribers already connected                                                                                                                  |
| `WORKER_CONFLICT`          | 409     | Card already queued or running                                                                                                                                                  |
| `WORKER_NOT_RUNNING`       | 409     | Card is not running; also a cold chat session                                                                                                                                   |
| `BACKEND_DISABLED`         | 503     | No task backend (or no such backend) configured                                                                                                                                 |
| `BACKEND_UNAVAILABLE`      | 502     | Backend webhook or probe failed                                                                                                                                                 |
| `BACKEND_NOT_FOUND`        | 404     | `{backend}` is not `agent` or `chat`                                                                                                                                            |
| `INVALID_SIGNATURE`        | 403     | HMAC signature or `X-Webhook-Timestamp` missing, wrong, replayed, or outside the 5-minute skew window                                                                           |
| `CONTENT_TOO_LARGE`        | 413     | Message, request body, or image exceeds the size cap                                                                                                                            |
| `NO_GITHUB_REPO`           | 404     | Project `repo` is not a GitHub URL                                                                                                                                              |
| `SYNC_DISABLED`            | 503     | Sync trigger with no remote configured                                                                                                                                          |
| `SYNC_ERROR`               | 500     | Sync cycle raised an error                                                                                                                                                      |
| `REMOTE_UNREACHABLE`       | 503     | Shared boards: a push-verified write could not reach the remote; the board is unchanged, retry                                                                                  |
| `PLAYBOOK_NOT_FOUND`       | 404     | Unknown playbook id                                                                                                                                                             |
| `PLAYBOOK_ENTRY_NOT_FOUND` | 404     | Unknown entry id                                                                                                                                                                |
| `PLAYBOOK_ENTRY_EXISTS`    | 409     | Duplicate `{project, card}` entry                                                                                                                                               |
| `IMAGE_NOT_FOUND`          | 404     | Unknown or malformed image id                                                                                                                                                   |
| `IMAGE_UNSUPPORTED`        | 415     | Image format not png/jpeg/gif/webp (animated WebP lands here too)                                                                                                               |
| `IMAGE_ANIMATED`           | 415     | Multi-frame GIF                                                                                                                                                                 |
| `IMAGE_MISSING_FILE`       | 400     | Multipart form missing the `file` field                                                                                                                                         |
| `IMAGE_INVALID_PAYLOAD`    | 400     | Malformed multipart body                                                                                                                                                        |
| `MODEL_NOT_BLACKLISTED`    | 404     | `DELETE /api/admin/model-blacklist/{slug}` for a slug not on the list                                                                                                           |
| `INTERNAL_ERROR`           | 500/502 | Unhandled server error (500); credential mint failure (502)                                                                                                                     |

**Error codes relevant to vetting:**

| Code               | HTTP | When                                                                                                                                                      |
| ------------------ | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CARD_NOT_VETTED`  | 403  | A non-human agent calls `POST /claim` on a card with `source != null && vetted == false`.                                                                 |
| `HUMAN_ONLY_FIELD` | 403  | A caller without the `human:` prefix sets a human-only field on card create, PUT, or PATCH (list under § Card Endpoints), or calls a human-only endpoint. |

## Authentication (multi mode)

`/api/auth/*`, `/api/users`, and the user, credential, and chat routes under
`/api/admin/*` are registered only when `auth.mode: multi` (the config
default - see the `auth` block in `config.yaml.example`). In `auth.mode:
none` the router skips these routes and the `sessionGuard` middleware
entirely, so a request against them gets a plain `404 page not found` (no
JSON body), not `401`. See § Trust model in [architecture.md](architecture.md)
for the security-review framing; this section documents the wire contract.
Setup and operations are in [authentication.md](authentication.md).

**Exception:** the model-outcomes and model-blacklist pairs
(`GET`/`DELETE /api/admin/model-outcomes`, `GET /api/admin/model-blacklist`,
`DELETE /api/admin/model-blacklist/{slug...}`) are registered in **both**
auth modes - model-selection feedback tracking does not depend on the auth
system. They are documented at the end of this section.

**Session gate.** `sessionGuard` runs on every request in multi mode and
rejects any request with no valid session - reads as well as writes. A
rejected cookie is cleared in the 401 response. Exempt paths (reachable
without a session):

| Path                                       | Why                                                      |
| ------------------------------------------ | -------------------------------------------------------- |
| `/healthz`, `/readyz`                      | Probes                                                   |
| `/mcp`                                     | Bearer-authed                                            |
| `/api/auth/*`                              | Login flow (`/session` and `/password` self-enforce 401) |
| `/api/app/config`                          | Serves a slim pre-login payload                          |
| `/api/agent/*`, `/api/chat/*`, `/api/v1/*` | HMAC-signed backend callbacks                            |
| `/api/worker/*`                            | Bearer-authed worker channel (§ Worker Endpoints)        |

The browser-facing `/api/worker/logs` and `/api/backend/health` are carved
out of those prefixes and do require a session.

**Admin gate.** `requireAdmin` layers on top of the session gate. It guards
every `/api/admin/*` route below plus five other call sites: `POST
/api/projects`, `PUT /api/projects/{project}`, `DELETE
/api/projects/{project}`, `POST /api/projects/{project}/recalculate-costs`,
and `GET /api/backends/{backend}/images`. Ordinary card work - claim, release,
update, transition, activity - needs only a valid session, any role.

**The 401 / 403 contract:**

| Status | Code           | Meaning                                                                                                        |
| ------ | -------------- | -------------------------------------------------------------------------------------------------------------- |
| 401    | `UNAUTHORIZED` | No session cookie, or the session is invalid/expired/for a disabled user. The SPA redirects to the login page. |
| 403    | `FORBIDDEN`    | A valid session exists but the user is not an admin, on an admin-gated route.                                  |

Neither code is returned in `auth.mode: none`.

**Session cookie:** `cm_session` - `HttpOnly`, `SameSite=Lax` always,
`Secure` when the request arrived over TLS (directly or via
`X-Forwarded-Proto: https`). The value is a random 256-bit token; only its
SHA-256 hash is persisted, so a stolen `auth.db` yields no usable session. A
session idle for more than 5 minutes gets a sliding renewal to `now +
auth.session_idle_ttl` (default `720h`) on its next validated request, and the
response re-sets the cookie with the refreshed `Max-Age`.

**CSRF still applies.** `/api/auth/*` and `/api/admin/*` are not on the
CSRF-exempt list: every non-GET call, including `POST /api/auth/login`, needs
`X-Requested-With: contextmatrix` or is rejected with 403 `BAD_REQUEST` before
credentials are checked.

The HMAC-signed backend channels (`/api/agent/*`, `/api/chat/*`, `/api/v1/*`)
and the bearer-authed `GET /api/worker/git-credentials` exist in both auth
modes and are unrelated to sessions - see § Worker & Backend Endpoints and
§ Worker Endpoints.

### POST /api/auth/login

```json
{ "username": "alice", "password": "correct-horse-battery" }
```

On success, sets the `cm_session` cookie and returns **200** with the session
identity:

```json
{ "username": "alice", "display_name": "Alice Nakamura", "is_admin": true }
```

Failures are uniform: an unknown username, a disabled account, a wrong
password, and an account whose invite was never redeemed all return **401
`UNAUTHORIZED`** ("invalid credentials"). A username longer than 32
characters is rejected the same way before the limiter is consulted.

Repeated failures for the same (normalized username, client IP) pair trip an
in-memory rate limiter: the first two failures are free, the third blocks for
1 second, doubling per further failure up to a 5-minute cap. A blocked
attempt returns **429 `RATE_LIMITED`** with a `Retry-After` header (whole
seconds, rounded up).

Each login runs a memory-hard argon2id derivation, so the login path is
capped to four concurrent derivations (peak 256Mi). A request that arrives
while all four slots are held is rejected immediately with **503
`LOGIN_BUSY`** and `Retry-After: 1` - never queued, and never counted against
the account+IP limiter.

The client IP is the TCP peer address; `X-Forwarded-For` is not consulted,
since honoring it without a trusted-proxy allowlist would let any client
spoof its limiter key. Behind a reverse proxy all logins share the proxy's IP
in limiter keys - the per-account half still applies.

```bash
curl -i -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: contextmatrix' \
  -d '{"username":"alice","password":"correct-horse-battery"}'
```

### POST /api/auth/logout

No request body - deletes the session behind the `cm_session` cookie (if any)
and clears the cookie. Idempotent: always **204 No Content**.

### GET /api/auth/session

"Who am I." Router-exempt from the session gate (so the login page can probe
it without a redirect loop), but the handler enforces auth itself: **401
`UNAUTHORIZED`** with no valid session, otherwise **200** with the same shape
as the login response.

### GET /api/auth/token/{token}

Inspects a one-time token (bootstrap / invite / password-reset link) without
consuming it, so the redemption page can render the right form.

```json
{ "purpose": "invite", "username": "bob" }
```

`purpose` is `bootstrap`, `invite`, or `reset`. `username` is always present
and is `""` for a bootstrap token (the account does not exist yet).

**Errors:**

| Status | Code            | When                                           |
| ------ | --------------- | ---------------------------------------------- |
| 404    | `TOKEN_INVALID` | Unknown token                                  |
| 410    | `TOKEN_INVALID` | Token already redeemed or past its 48-hour TTL |

### POST /api/auth/token/{token}

Redeems the token: sets a password and logs the user in.

```json
{ "username": "bob", "display_name": "Bob Okafor", "password": "correct-horse-battery" }
```

`username` / `display_name` apply only to a `bootstrap` token, where they
create the first admin account (`is_admin: true` unconditionally). For
`invite` / `reset` tokens only `password` is read; redeeming a `reset` token
also kills every other live session for the account.

On success: sets the `cm_session` cookie, returns **200** with the session
identity (same shape as login). Password and username validation run before
the token is consumed, so a 422 does not burn the link; the bootstrap-closed
check runs after consumption, so that 409 does.

**Errors:**

| Status | Code               | When                                                                                             |
| ------ | ------------------ | ------------------------------------------------------------------------------------------------ |
| 400    | `BAD_REQUEST`      | Malformed JSON body                                                                              |
| 404    | `TOKEN_INVALID`    | Unknown token                                                                                    |
| 409    | `VALIDATION_ERROR` | Bootstrap token redeemed after a user already exists                                             |
| 410    | `TOKEN_INVALID`    | Token already redeemed or expired                                                                |
| 422    | `VALIDATION_ERROR` | `password` under 10 characters                                                                   |
| 422    | `VALIDATION_ERROR` | (bootstrap only) username invalid - "1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation" |
| 422    | `VALIDATION_ERROR` | (bootstrap only) username already taken                                                          |

### POST /api/auth/password

Self-service password change for the logged-in caller. Router-exempt like
`/session`; the handler enforces auth itself.

```json
{ "current_password": "correct-horse-battery", "new_password": "another-correct-horse" }
```

Checks `new_password` length first, then verifies `current_password`, sets
the new one, and deletes every other session for the account - the calling
session survives. Returns **204 No Content**.

**Errors:**

| Status | Code               | When                                       |
| ------ | ------------------ | ------------------------------------------ |
| 401    | `UNAUTHORIZED`     | No session, or `current_password` is wrong |
| 422    | `VALIDATION_ERROR` | `new_password` under 10 characters         |

### GET /api/users

The user roster backing the card `assignee` pickers. Any valid session may
call it - not admin-gated. Only registered in `auth.mode: multi`.

```json
[
  { "username": "alice", "display_name": "Alice Nakamura" },
  { "username": "bob", "display_name": "Bob Okafor" }
]
```

Sorted by `username`; disabled accounts are excluded; `[]` when empty. No
`is_admin`, `disabled`, `has_password`, or `last_login_at` - this exists to
populate a picker, not to manage accounts.

**Admin endpoints.** Every route below additionally requires an admin session
(**403 `FORBIDDEN`** otherwise).

### GET /api/admin/users

Lists every account, ordered by username.

```json
[
  {
    "username": "alice",
    "display_name": "Alice Nakamura",
    "is_admin": true,
    "disabled": false,
    "has_password": true,
    "last_login_at": "2026-07-01T09:12:00Z"
  }
]
```

`has_password` is `false` for a user who has not yet redeemed their invite
link. `last_login_at` is omitted until the first successful login.

### POST /api/admin/users

Creates an account with no password and mints an invite link.

```json
{ "username": "bob", "display_name": "Bob Okafor", "is_admin": false }
```

Response **201**:

```json
{
  "user": { "username": "bob", "display_name": "Bob Okafor", "is_admin": false, "disabled": false, "has_password": false },
  "invite": { "token": "raw-one-time-token", "purpose": "invite", "expires_at": "2026-07-05T12:00:00Z" }
}
```

The server does not know its own public address, so it returns the raw token;
the admin UI builds the `/auth/token/<token>` frontend route from it. The
token is valid for 48 hours - use `POST /api/admin/users/{username}/invite`
to mint a replacement.

**Errors:** `422 VALIDATION_ERROR` - username already taken, or invalid
("1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation").

### PATCH /api/admin/users/{username}

Partial update - only keys present in the body are applied, in the order
`display_name`, `is_admin`, `disabled`:

```json
{ "display_name": "Bob O.", "is_admin": true, "disabled": false }
```

Demoting or disabling the last active admin is refused before any field is
applied. Disabling a user deletes their sessions immediately; re-enabling does
not restore them. Returns **200** with the updated account (same shape as the
list).

**Errors:**

| Status | Code               | When                           |
| ------ | ------------------ | ------------------------------ |
| 404    | `USER_NOT_FOUND`   | Unknown username               |
| 409    | `VALIDATION_ERROR` | Would leave zero active admins |

### POST /api/admin/users/{username}/invite

Mints a fresh one-time link, invalidating any earlier unused link of the same
purpose. Purpose is `invite` if the account has never had a password, `reset`
otherwise.

```json
{ "token": "raw-one-time-token", "purpose": "reset", "expires_at": "2026-07-05T12:00:00Z" }
```

**Errors:** `404 USER_NOT_FOUND`.

### GET /api/admin/credentials

Lists the GitHub credential pool - metadata only; secrets never leave the
server.

```json
[
  {
    "name": "org-app",
    "kind": "app",
    "host": "",
    "api_base_url": "",
    "app_id": 123456,
    "installation_id": 78901234,
    "created_by": "human:alice",
    "disabled": false,
    "created_at": "2026-06-01T00:00:00Z",
    "updated_at": "2026-06-01T00:00:00Z",
    "last_used_at": "2026-07-02T14:00:00Z"
  }
]
```

`kind` is `app` or `pat`. `last_used_at` is omitted until the credential is
first resolved for a token. A project binds to one pool entry via its
`.board.yaml` `github_credential` field - see [data-model.md](data-model.md).

### POST /api/admin/credentials

Validates the credential's shape, checks it live against GitHub (an `app`
entry mints an installation token; a `pat` entry probes `/rate_limit`),
encrypts the secret, and stores it.

```json
{
  "name": "org-app",
  "kind": "app",
  "host": "",
  "api_base_url": "",
  "app_id": 123456,
  "installation_id": 78901234,
  "secret": "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
}
```

For `kind: "pat"`, `secret` is the token string and `app_id` /
`installation_id` are ignored. `name` must be 1-64 chars, a-z 0-9 . _ -, with
no leading/trailing punctuation. `created_by` is derived from the session.
Response **201** with the same shape as the list.

**Errors:**

| Status | Code               | When                                                                                               |
| ------ | ------------------ | -------------------------------------------------------------------------------------------------- |
| 422    | `VALIDATION_ERROR` | Bad shape (name, empty secret, `app` entry missing `app_id`/`installation_id`, key does not parse) |
| 422    | `VALIDATION_ERROR` | Credential rejected by the live GitHub check, or `name` already taken                              |
| 500    | `INTERNAL_ERROR`   | Auth master key not configured                                                                     |

### PUT /api/admin/credentials/{name}

Rotate the secret, update metadata, and/or toggle `disabled` - any subset in
one call, applied in that order (metadata, then secret, then disabled). Every
field is optional:

```json
{ "secret": "ghp_...", "host": "", "api_base_url": "", "app_id": 1, "installation_id": 2, "disabled": false }
```

A metadata change re-validates against GitHub using the stored secret merged
with the new metadata; a secret rotation re-validates the new secret against
the stored metadata. Returns **200** with the updated credential.

**Errors:**

| Status | Code               | When                                                        |
| ------ | ------------------ | ----------------------------------------------------------- |
| 404    | `VALIDATION_ERROR` | Unknown credential `name`                                   |
| 422    | `VALIDATION_ERROR` | Empty `secret` on rotate, or GitHub rejected the credential |
| 500    | `INTERNAL_ERROR`   | Auth master key not configured                              |

### DELETE /api/admin/credentials/{name}

Refuses to delete a credential that any project's `.board.yaml`
`github_credential` still references:

```json
{ "error": "credential is bound to projects", "code": "VALIDATION_ERROR", "details": "rebind first: alpha, beta" }
```

**409** in that case. Otherwise **204 No Content**, or **404
`VALIDATION_ERROR`** for an unknown `name`.

### GET /api/admin/chats

Lists every chat session on the instance - no owner scoping. Metadata and
cost totals only; transcript content is never included. Query parameters
`project`, `status`, and `limit` behave as on `GET /api/chats` (default 500,
max 5000, bad values 400); `created_by` is ignored. Registered only when a
chat manager is wired.

**Response:** `200 OK` - array of session objects (same shape as
`GET /api/chats`).

### POST /api/admin/chats/{id}/end

Force-ends any session regardless of owner - the remedy when a stuck active
session holds a slot of the global concurrency cap. Same semantics as `POST
/api/chats/{id}/end`; returns the updated session.

**Errors:** `404 CHAT_NOT_FOUND`, `403 FORBIDDEN`.

### DELETE /api/admin/chats/{id}

Deletes any session regardless of owner. Cost tombstones survive, so
dashboard aggregates stay accurate.

**Response:** `204 No Content`, `404 CHAT_NOT_FOUND`.

There is deliberately no admin route that returns transcript content (no
messages, stream, or open).

### GET /api/admin/model-outcomes

Registered in **both** auth modes: open in `auth.mode: none` (same trust
posture as project management), admin-gated in `multi` (**403
`FORBIDDEN`** otherwise).

Returns the per-model outcome ledger recorded via the MCP
`report_model_outcome` tool - an observability surface only; model selection
never reads it ([model-selection.md](model-selection.md)). Race rows
(`n_candidates > 1`, Best-of-N head-to-head results) and solo rows
(single-model runs) aggregate separately - a solo completion is not a win
over anything, so there is no combined win rate:

```json
{
  "total_samples": 84,
  "models": [
    {
      "model": "deepseek/deepseek-v4-flash",
      "race_samples": 8,
      "race_wins": 5,
      "race_win_rate": 0.625,
      "solo_samples": 14,
      "solo_failures": 2,
      "total_cost_usd": 1.42
    }
  ]
}
```

`race_win_rate` is computed over race rows alone and is `0` when
`race_samples` is `0`. `total_samples` sums race and solo rows across all
models. `models` is `[]` when empty.

### DELETE /api/admin/model-outcomes

Deletes every recorded outcome row. Returns **200 OK** with the row count:

```json
{ "deleted": 84 }
```

### GET /api/admin/model-blacklist

Same registration and gating as the model-outcomes pair. Returns every model
the agent backend has reported incapable via the MCP `report_incapable_model`
tool. Blacklisted models are excluded from every automatic pick; only a card
pin overrides ([model-selection.md](model-selection.md) § The blacklist).
Timestamps are unix seconds; `sample_card` is omitted when the report carried
none:

```json
{
  "models": [
    {
      "slug": "moonshotai/kimi-k3",
      "reason": "tool calls failed to parse on 3 consecutive turns",
      "sample_card": "CM-101",
      "reported_by": "agent:worker-1",
      "first_seen": 1756400000,
      "last_seen": 1756500000
    }
  ]
}
```

### DELETE /api/admin/model-blacklist/{slug...}

Removes one model from the blacklist. The path parameter is a rest wildcard
because model slugs contain a slash - request
`DELETE /api/admin/model-blacklist/moonshotai/kimi-k3` literally, no
URL-encoding. Returns **200 OK** with the deleted slug:

```json
{ "deleted": "moonshotai/kimi-k3" }
```

**Errors:** `404 MODEL_NOT_BLACKLISTED`.

## Health Endpoints

### GET /healthz

Shallow liveness probe. Always `200 OK` with body `{"status":"ok"}`
(`Content-Type: application/json`) while the process is running. No
dependency checks. Use it as a `livenessProbe`; do not gate traffic on it.

```bash
curl http://localhost:8080/healthz
# → {"status":"ok"}
```

### GET /readyz

Dependency-checked readiness probe. Every check runs, with a 500 ms timeout:

| Check         | What it tests                                                                                             |
| ------------- | --------------------------------------------------------------------------------------------------------- |
| `store`       | `ListProjects` succeeds (boards directory is readable)                                                    |
| `git`         | `CurrentBranch` resolves on the first boards repo; every further repo adds a `git:<repo>` check           |
| `session_log` | Always `ok: true`. A nil session-log manager means no task backend is configured, which is still healthy. |

Returns **200** when all checks pass, **503** when any fails.

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

On failure `status` is `"degraded"` and the failing check carries an `error`
string:

```json
{ "name": "store", "ok": false, "error": "open /data/boards: permission denied" }
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

## Card Endpoints

Field semantics (states, types, human-only rules, `verify`, mob and
Best-of-N fields) live in [data-model.md](data-model.md); this section covers
the wire shapes. The human side of running a card is in
[running-cards.md](running-cards.md).

### Card object

Every card-returning REST endpoint uses this shape. Fields marked `?` are
omitted when zero or empty; the rest are always present.

| Field                                                    | Type                    | Notes                                                                                                                                                               |
| -------------------------------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`, `title`, `project`, `type`, `state`, `priority`    | string                  |                                                                                                                                                                     |
| `body`                                                   | string                  | Markdown; always present (may be `""`)                                                                                                                              |
| `assigned_agent?`, `last_heartbeat?`                     | string, RFC 3339        | Current claim                                                                                                                                                       |
| `claimed_via?`, `claimed_at?`, `claim_epoch?`            | string, RFC 3339, int   | Shared boards only: granting instance, time, and the fence bumped on every claim, release, stall, force-release, and terminal transition                            |
| `parent?`, `subtasks?`, `depends_on?`                    | string, string[]        |                                                                                                                                                                     |
| `dependencies_met?`                                      | bool                    | Computed on read                                                                                                                                                    |
| `blocked_by?`                                            | string[]                | Computed on read: the `depends_on` ids not yet `done`; absent when all are met                                                                                      |
| `context?`, `labels?`                                    | string[]                |                                                                                                                                                                     |
| `skills?`                                                | string[]                | `null`/absent = project default; `[]` = mount no skills                                                                                                             |
| `source?`                                                | object                  | `{system, external_id, external_url}` for imported cards                                                                                                            |
| `custom?`                                                | object                  | Free-form                                                                                                                                                           |
| `assignee?`                                              | string                  | Multi mode username                                                                                                                                                 |
| `autonomous`, `vetted`                                   | bool                    | Always present                                                                                                                                                      |
| `model_orchestrator?`, `model_coder?`, `model_reviewer?` | string                  | Model pins                                                                                                                                                          |
| `best_of_n?`, `max_capability?`                          | int, bool               |                                                                                                                                                                     |
| `mob_participants?`, `mob_phases?`, `mob_guests?`        | int, string[], string[] |                                                                                                                                                                     |
| `verify?`                                                | object                  | Card-level verify override                                                                                                                                          |
| `create_pr?`, `await_ci?`, `await_copilot_review?`       | bool                    |                                                                                                                                                                     |
| `branch_name?`, `base_branch?`, `pr_url?`                | string                  |                                                                                                                                                                     |
| `review_attempts?`                                       | int                     |                                                                                                                                                                     |
| `worker_status?`                                         | string                  | `queued`, `running`, `completed`, `failed`, `killed`, `parked`                                                                                                      |
| `phase?`                                                 | string                  |                                                                                                                                                                     |
| `token_usage?`                                           | object                  | `{model?, prompt_tokens, completion_tokens, cache_read_tokens?, cache_creation_tokens?, estimated_cost_usd}`                                                        |
| `usage_breakdown?`                                       | object[]                | Per `(agent, model)` buckets: `{agent, model, prompt_tokens, completion_tokens, cache_read_tokens?, cache_creation_tokens?, cost_usd, cost_source, counts_source?}` |
| `subtask_cost_usd?`, `subtask_cost_has_estimates?`       | float, bool             | Computed on `GET .../cards/{id}` only                                                                                                                               |
| `in_playbooks?`                                          | string[]                | Computed on read                                                                                                                                                    |
| `created`, `updated`                                     | RFC 3339                |                                                                                                                                                                     |
| `activity_log?`                                          | object[]                | `{agent, ts, action, message, skill?}`                                                                                                                              |

### GET /api/projects/{project}/cards/{id}

Returns a single card. `subtask_cost_usd` is present here for a card with
costed direct subtasks - computed on read, omitted when zero - and absent from
list responses.

### Card list query parameters

| Parameter     | Values           | Description                                                                                                                   |
| ------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `state`       | state name       | Filter by card state; a value outside the project config is 400                                                               |
| `type`        | type name        | Filter by card type (`subtask` is always accepted); unknown values are 400                                                    |
| `label`       | label string     | Filter cards that have this label                                                                                             |
| `agent`       | agent ID         | Filter by `assigned_agent`                                                                                                    |
| `parent`      | card ID          | Filter by parent card (upper-cased server-side)                                                                               |
| `priority`    | priority name    | Filter by priority; unknown values are 400                                                                                    |
| `external_id` | external ID      | Filter by `source.external_id` (idempotent import check)                                                                      |
| `vetted`      | `true` / `false` | Filter by `vetted`; any value other than `true` means `false`. `?vetted=false` lists unvetted external cards awaiting review. |
| `limit`       | 1-2000           | Maximum items in the response page. Default `500`. Out-of-range or non-integer values are 400.                                |
| `cursor`      | opaque string    | Page continuation token from the previous response's `next_cursor`.                                                           |

### Card list response envelope

`GET /api/projects/{project}/cards` returns a JSON object (not a bare array):

```json
{
  "items": [{ "id": "PROJ-001", "...": "..." }],
  "next_cursor": "UFJPSi0wMDE",
  "total": 1234
}
```

- `items` - page of cards, ordered by ID ascending. Always present (may be
  `[]`).
- `next_cursor` - opaque base64url token; pass back in `?cursor=` to fetch the
  next page. Omitted on the last page.
- `total` - total un-filtered card count for the project. Emitted **only on the
  first page** (when the request has no `cursor`).

Cursors encode the last card ID of the page and are stable across filter
changes. Invalid cursors return 400 `BAD_REQUEST`. The server sorts before
slicing, so walking `next_cursor` to exhaustion visits every matching card
exactly once.

```bash
# First page - 1 item, includes total.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1"
# → {"items":[{"id":"ALPHA-001", ...}],"next_cursor":"QUxQSEEtMDAx","total":3}

# Follow-up pages use cursor.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1&cursor=QUxQSEEtMDAx"
# → {"items":[{"id":"ALPHA-002", ...}],"next_cursor":"QUxQSEEtMDAy"}

# Last page - next_cursor omitted.
curl "http://localhost:8080/api/projects/alpha/cards?limit=1&cursor=QUxQSEEtMDAy"
# → {"items":[{"id":"ALPHA-003", ...}]}
```

### POST /api/projects/{project}/cards

Creates a card in the project's first state. Only `title` is required.

```json
{
  "title": "Add rate limiting to the login endpoint",
  "type": "task",
  "priority": "high",
  "labels": ["security"],
  "parent": "",
  "body": "## Context\n...",
  "assignee": "alice",
  "source": { "system": "github", "external_id": "42", "external_url": "https://github.com/org/repo/issues/42" },
  "autonomous": false,
  "create_pr": true,
  "await_ci": false,
  "await_copilot_review": false,
  "base_branch": "main",
  "vetted": true,
  "skills": ["go-development"],
  "model_orchestrator": "",
  "model_coder": "",
  "model_reviewer": "",
  "best_of_n": 0,
  "max_capability": false,
  "mob_participants": 0,
  "mob_phases": [],
  "mob_guests": [],
  "verify": { "command": "make test", "timeout_seconds": 600 }
}
```

`create_pr` defaults to `true` when omitted. `skills` omitted means the
project default; `[]` mounts no skills. `depends_on`, `subtasks`, `context`,
and `custom` are not accepted on create - set them with PUT or PATCH.

**Human-only fields** (403 `HUMAN_ONLY_FIELD` when a non-`human:` caller sets
any of them, on create, PUT, and PATCH alike): `autonomous`, `create_pr`,
`await_ci`, `await_copilot_review`, `vetted`, `assignee`, `base_branch`,
`model_orchestrator`, `model_coder`, `model_reviewer`, `best_of_n`,
`max_capability`, `mob_participants`, `mob_phases`, `mob_guests`, `verify`.
On create and PATCH the check runs before the card is loaded. PUT compares
against the stored values, so clearing a human-only field is also gated; PUT
does not accept `base_branch` or `verify` at all.

**Response:** 201 with the full card.

**Errors:**

| Status | Code                                    | When                                                                                                                                                           |
| ------ | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 400    | `BAD_REQUEST`                           | Bad JSON, empty `title`, `best_of_n` outside `0` or `2..max_candidates`, invalid mob fields                                                                    |
| 400    | `VALIDATION_ERROR`                      | A `skills` name not in `task_skills.dir` (or outside the project's `default_skills`)                                                                           |
| 403    | `HUMAN_ONLY_FIELD`                      | See above                                                                                                                                                      |
| 404    | `PROJECT_NOT_FOUND`, `PARENT_NOT_FOUND` |                                                                                                                                                                |
| 409    | `CARD_EXISTS`                           | ID collision                                                                                                                                                   |
| 422    | `VALIDATION_ERROR`                      | Unknown type/priority, bad `external_url`, model pin not in catalog, field too long, invalid `verify`, bad `assignee` (unknown, disabled, or set in none mode) |
| 503    | `REMOTE_UNREACHABLE`                    | Shared boards: push could not be verified                                                                                                                      |

### PUT /api/projects/{project}/cards/{id}

Full replacement. Accepts `title`, `type`, `state`, `priority`, `labels`,
`parent`, `subtasks`, `depends_on`, `context`, `custom`, `body`, `assignee`,
`autonomous`, `create_pr`, `await_ci`, `await_copilot_review`, `vetted`,
`skills`, `phase`, the three model pins, `best_of_n`, `max_capability`, and
the three mob fields as plain values; every omitted field is written as its
zero value (`skills` omitted resets to the project default). `phase` is the
exception: omitted leaves it unchanged. A `state` change goes through
transition validation.

**Response:** 200 with the full card.

**Errors:** the create set plus 404 `CARD_NOT_FOUND`, 403 `AGENT_MISMATCH`
(the card is claimed and the caller is not the owner, or sends no
`X-Agent-ID`), 409 `INVALID_TRANSITION`, and 409 `DEPENDENCIES_NOT_MET`.

### PATCH /api/projects/{project}/cards/{id}

Partial update - only keys present in the body are applied.

```json
{ "state": "in_progress", "labels": ["urgent"], "depends_on": ["PROJ-001"], "skills_clear": true }
```

| Field                                                                                     | Semantics                                                                                                  |
| ----------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `title`, `type`, `state`, `priority`, `body`, `assignee`, `base_branch`, `phase`          | string; `state` runs transition validation                                                                 |
| `labels`, `mob_phases`, `mob_guests`                                                      | `[]` clears; omitted leaves unchanged                                                                      |
| `depends_on`                                                                              | Replaces the list; `[]` clears; cycles, self, cross-project, or unknown IDs are 409 `DEPENDENCIES_NOT_MET` |
| `skills`                                                                                  | Explicit list or `[]`                                                                                      |
| `skills_clear`                                                                            | `true` resets `skills` to absent (project default) - JSON cannot distinguish an omitted key from `null`    |
| `autonomous`, `create_pr`, `await_ci`, `await_copilot_review`, `vetted`, `max_capability` | bool                                                                                                       |
| `model_orchestrator`, `model_coder`, `model_reviewer`, `best_of_n`, `mob_participants`    | Pins and counts; mob fields are validated against the resulting card                                       |
| `verify`                                                                                  | Replaces the whole object; a zero-value object clears it                                                   |

The commit author, activity entry, and SSE event carry the resolved caller
identity. **Response:** 200 with the full card. **Errors:** as PUT.

### DELETE /api/projects/{project}/cards/{id}

Deletes the card file (git history keeps it). **204 No Content**.

**Errors:** 404 `CARD_NOT_FOUND`; 403 `AGENT_MISMATCH` on a claimed card the
caller does not own; 422 `VALIDATION_ERROR` when the card still has subtasks
("cannot delete card with N subtask(s)").

### POST /api/projects/{project}/cards/{id}/claim

Claims the card for the caller (`X-Agent-ID`, or the session identity in
multi mode) and stamps `last_heartbeat`. The state is not changed - the MCP
`claim_card` tool is what auto-transitions to `in_progress`. Empty body
allowed; any body is ignored. **200** with the full card.

**Errors:**

| Status | Code                 | When                                                                           |
| ------ | -------------------- | ------------------------------------------------------------------------------ |
| 400    | `BAD_REQUEST`        | No `X-Agent-ID` and no session                                                 |
| 403    | `CARD_NOT_VETTED`    | Non-human caller on an external card with `vetted: false`                      |
| 409    | `ALREADY_CLAIMED`    | Held by another agent; on a shared board the details name the holding instance |
| 409    | `INVALID_TRANSITION` | Card is `done` or `not_planned` and the caller does not already hold the claim |
| 422    | `VALIDATION_ERROR`   | Agent ID longer than 256 characters                                            |
| 503    | `REMOTE_UNREACHABLE` | Shared boards: the claim could not be pushed                                   |

### POST /api/projects/{project}/cards/{id}/release

Releases the caller's claim and flushes any deferred commits. **200** with the
full card.

**Errors:** 400 `BAD_REQUEST` (no identity); 409 `NOT_CLAIMED`; 403
`AGENT_MISMATCH` (not the owner, or the lease was fenced by a peer instance).

### POST /api/projects/{project}/cards/{id}/force-release

Human-only. Clears another agent's claim (crashed-worker recovery): a
`queued`/`running` worker status becomes `failed` and a `force_released`
activity entry is appended. A missing `X-Agent-ID` defaults to `human:api`.
**200** with the full card.

**Errors:** 403 `HUMAN_ONLY_FIELD`; 409 `NOT_CLAIMED`; 503
`REMOTE_UNREACHABLE` on shared boards.

## App Endpoints

### GET /api/task-skills

Returns the task skills available in the configured `task_skills.dir`. Each
entry has a `name` (the skill directory name) and a `description` (from the
skill's `SKILL.md` frontmatter), sorted by name.

```json
{
  "skills": [
    { "name": "documentation", "description": "Use when writing or updating documentation files." },
    { "name": "go-development", "description": "Use when implementing or modifying Go source files." }
  ]
}
```

Returns `{"skills": []}` if `task_skills.dir` is not configured or empty; 500
`INTERNAL_ERROR` if the directory cannot be read. Feeds the project-default
and per-card skill selectors in the UI.

### GET /api/app/config

Server-side settings the SPA needs before it renders. Exempt from the session
guard in multi mode, but an unauthenticated caller there gets only the slim
payload.

**Full payload** (`none` mode always; `multi` mode with a session):

```json
{
  "theme": "everforest",
  "version": "v0.42.0",
  "auth_mode": "multi",
  "task_backend": "agent",
  "favorites": { "complex": ["anthropic/claude-opus-4.8"] },
  "best_of_n_max": 5,
  "best_of_n_default": 3,
  "mob_max_participants": 5,
  "mob_default_participants": 3,
  "mob_guest_names": ["laptop"],
  "mob_execute_checkpoints": true,
  "chat_enabled": true,
  "instance_id": "laptop-3f9a2c",
  "shared_boards": true,
  "boards_repos": [{ "name": "team", "shared": true }, { "name": "private", "shared": false }]
}
```

| Field                                              | Presence        | Meaning                                                                                            |
| -------------------------------------------------- | --------------- | -------------------------------------------------------------------------------------------------- |
| `theme`                                            | always          | `everforest` (default), `radix`, or `catppuccin`; the SPA sets `data-palette` on `<html>` to match |
| `version`                                          | always          | Build version; `""` when built without the version ldflag                                          |
| `auth_mode`                                        | always          | `multi` or `none`                                                                                  |
| `task_backend`                                     | full only       | `"agent"` when a task backend is configured, else `""`                                             |
| `favorites`                                        | full, omitempty | Operator-configured preferred model slugs per tier (pin presets)                                   |
| `best_of_n_max`, `best_of_n_default`               | full, omitempty | Mirror `best_of_n.max_candidates` / `default_candidates` (defaults 5 and 3)                        |
| `mob_max_participants`, `mob_default_participants` | full, omitempty | Mirror `mob.max_participants` / `default_participants`                                             |
| `mob_guest_names`                                  | full, omitempty | Names from the `mob.guests` registry - never URLs or tokens                                        |
| `mob_execute_checkpoints`                          | full, omitempty | `true` when `mob.execute_checkpoints_enabled` is on; absent (not `false`) when off                 |
| `chat_enabled`                                     | full, always    | A chat backend entry is enabled with `url` and `api_key` set                                       |
| `instance_id`, `shared_boards`                     | full, omitempty | `instance.id` and whether any boards repo is shared; both drop out on a private board              |
| `boards_repos`                                     | full, omitempty | Every configured boards repository in config order, `{name, shared}`                               |

**Slim payload** (unauthenticated caller in `multi` mode) is a separate
struct with exactly `theme`, `version`, and `auth_mode`:

```json
{ "theme": "everforest", "version": "v0.42.0", "auth_mode": "multi" }
```

```bash
curl http://localhost:8080/api/app/config
# → {"theme":"everforest","version":"v0.42.0","auth_mode":"none","task_backend":"agent","best_of_n_max":5,"best_of_n_default":3,"chat_enabled":false}
```

### GET /api/models

Model catalog for the card model-pin pickers, independent of the chat mode.
`source` is `openrouter` (the vendor-screened OpenRouter list), `endpoint`
(the LLM endpoint's served list), or `none` (no catalog builder configured;
`models` is empty).

```json
{ "source": "openrouter", "models": [ { "id": "anthropic/claude-sonnet-4.5", "max_tokens": 200000 } ] }
```

Models on the blacklist (see `GET /api/admin/model-blacklist`) carry
`"blacklisted": true`; the key is absent from every other entry. The join is
best-effort: a failed blacklist read logs a warning and serves the list
unflagged. The flag is informational - pinning a blacklisted model stays
allowed by design ([model-selection.md](model-selection.md)).

Card model pins (`model_orchestrator` / `model_coder` / `model_reviewer` on
card create, PUT, and PATCH) are validated against this same served set:
a pin outside it returns 422 `VALIDATION_ERROR` ("model pin not in
catalog"). Only changed values are checked, and an empty or unfetched catalog
disables validation entirely (fail-open).

## Project Endpoints

Project field semantics are in [data-model.md](data-model.md); boards and
repos in [boards.md](boards.md) and [shared-boards.md](shared-boards.md).

### POST /api/projects

Admin-only in `auth.mode: multi` (403 `FORBIDDEN`). Creates a project.

```json
{
  "name": "my-project",
  "display_name": "My Project",
  "prefix": "MYPRO",
  "boards_repo": "team",
  "repo": "https://github.com/org/my-project.git",
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

| Field          | Required?   | Description                                                                                                                             |
| -------------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `name`         | conditional | Slug - directory name, URL segment, API identifier. Must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Derived from `display_name` when omitted. |
| `display_name` | conditional | Human-readable name; any printable characters. Stored in `.board.yaml`.                                                                 |
| `prefix`       | required    | Card ID prefix (`MYPRO` → `MYPRO-001`).                                                                                                 |
| `boards_repo`  | optional    | The boards repository to create the project in, by config name. Defaults to the first configured repo.                                  |
| `repo`         | optional    | Code repository URL.                                                                                                                    |

At least one of `name` or `display_name` is required. When `name` is
omitted, the server lowercases `display_name` and collapses runs of
non-alphanumerics to hyphens (`"My Project"` → `"my-project"`).

**Response:** 201 with the full `ProjectConfig`.

**Errors:**

| Status | Code                 | When                                                                                                      |
| ------ | -------------------- | --------------------------------------------------------------------------------------------------------- |
| 400    | `BAD_REQUEST`        | Neither `name` nor `display_name`; missing `prefix`; unknown `boards_repo`; the reserved name `playbooks` |
| 409    | `PROJECT_EXISTS`     | Derived or explicit slug already exists                                                                   |
| 422    | `VALIDATION_ERROR`   | Slug fails the pattern, or the states/transitions config is invalid                                       |
| 503    | `REMOTE_UNREACHABLE` | Shared boards: push could not be verified                                                                 |

### GET /api/projects / GET /api/projects/{project}

List all projects, or one by slug.

```json
{
  "name": "my-project",
  "display_name": "My Project",
  "prefix": "MYPRO",
  "next_id": 1,
  "boards_repo": "team",
  "repo": "https://github.com/org/my-project.git",
  "github_credential": "org-app",
  "states": ["..."],
  "types": ["..."],
  "priorities": ["..."],
  "transitions": { "...": [] },
  "remote_execution": { "worker_image": "...", "chat_worker_image": "..." },
  "github": { "...": "..." },
  "default_skills": ["go-development"],
  "verify": { "command": "make test", "timeout_seconds": 600 },
  "templates": { "...": "..." }
}
```

`name`, `prefix`, `next_id`, `states`, `types`, `priorities`, and
`transitions` are always present; everything else is omitted when unset
(`display_name` on older projects - fall back to `name`; `repos` is the
multi-repo form of `repo`).

### PUT /api/projects/{project}

Admin-only in `auth.mode: multi`. Updates the project configuration. `name`,
`display_name`, and `prefix` are immutable - recreate the project to change
them.

```json
{
  "repo": "https://github.com/org/my-project.git",
  "states": ["todo", "in_progress", "review", "done", "stalled", "not_planned"],
  "types": ["task", "bug"],
  "priorities": ["low", "medium", "high"],
  "transitions": { "todo": ["in_progress"], "...": [] },
  "github": { "...": "..." },
  "default_skills": ["go-development", "documentation"],
  "github_credential": "org-app",
  "verify": { "command": "make test", "timeout_seconds": 600, "env": ["JAVA_HOME"] },
  "remote_execution": {
    "worker_image": "my-org/go-worker:latest",
    "chat_worker_image": "my-org/go-chat-worker:latest"
  }
}
```

| Field               | Semantics                                                                                                                                                                                      |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `github`            | GitHub import configuration ([github-issue-import.md](github-issue-import.md))                                                                                                                 |
| `github_credential` | Binds the project's GitHub operations to a credential-pool entry (`GET /api/admin/credentials`). 422 `VALIDATION_ERROR` for an unknown name, or any non-empty value in `none` mode.            |
| `default_skills`    | Omitted or `null` clears (the backend mounts the full task-skills set); `[]` mounts none; a list constrains cards without their own `skills`. Unknown names: 400 `VALIDATION_ERROR`.           |
| `verify`            | Replace-whole-struct: omitted preserves, a present object replaces, a zero-value object clears. Invalid: 422 `VALIDATION_ERROR`. The card-level `verify` overrides it field by field.          |
| `remote_execution`  | Per-field merge: each of `worker_image` and `chat_worker_image` is independently omittable (preserves) or set; `""` clears back to the backend's default image. Charset and 512-byte cap: 422. |

Returns 200 with the updated `ProjectConfig`.

### DELETE /api/projects/{project}

Admin-only in `auth.mode: multi`. Deletes a project that has no cards.
**204 No Content**.

**Errors:** 404 `PROJECT_NOT_FOUND`; 409 `PROJECT_HAS_CARDS` (details:
`project "x" has N cards`); 503 `REMOTE_UNREACHABLE` on shared boards.

### GET /api/projects/{project}/branches

Returns a JSON array of branch names from the project's GitHub repository, for
the base-branch dropdown.

```json
["main", "develop", "release/v2", "feat/some-branch"]
```

| Status | Code               | When                                                        |
| ------ | ------------------ | ----------------------------------------------------------- |
| 404    | `NO_GITHUB_REPO`   | Project `repo` is not a GitHub URL                          |
| 422    | `VALIDATION_ERROR` | The project's bound credential cannot be resolved           |
| 500    | `INTERNAL_ERROR`   | GitHub fetch failed (auth, network, upstream); error logged |

### GET /api/projects/{project}/usage

Aggregated token usage across all cards in a project. Every field is always
present; `card_count` counts cards with a `token_usage` record.

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

### GET /api/projects/{project}/dashboard

```json
{
  "state_counts": { "todo": 3, "in_progress": 2, "done": 5 },
  "state_counts_parents": { "todo": 2, "in_progress": 1, "done": 3 },
  "active_agents": [
    { "agent_id": "claude-7a3f", "card_id": "ALPHA-003", "card_title": "...", "since": "...", "last_heartbeat": "..." }
  ],
  "total_cost_usd": 0.315,
  "total_cost_has_estimates": true,
  "total_cost_usd_last_30d": 0.284,
  "total_cost_has_estimates_last_30d": true,
  "total_cost_usd_prior_30d": 0.241,
  "cost_series_30d": [0.003, 0.012, 0.008, "... 30 daily buckets, oldest first"],
  "cards_completed_today": 2,
  "cards_completed_today_parents": 1,
  "cards_completed_last_7d": 9,
  "cards_completed_last_7d_parents": 4,
  "cards_completed_prior_7d": 6,
  "cards_completed_prior_7d_parents": 3,
  "metric_series": {
    "active_agents":     [0, 1, 1, 2, 2, 1, 3, 2],
    "in_flight":         [1, 2, 2, 3, 3, 2, 4, 3],
    "stalled":           [0, 0, 1, 1, 0, 0, 0, 0],
    "shipped":           [1, 0, 2, 1, 1, 2, 1, 2],
    "in_flight_parents": [1, 1, 1, 2, 2, 1, 2, 2],
    "stalled_parents":   [0, 0, 0, 1, 0, 0, 0, 0],
    "shipped_parents":   [0, 0, 1, 1, 0, 1, 1, 1]
  },
  "model_costs_30d": [
    { "model": "claude-sonnet-4-5", "prompt_tokens": 25000, "completion_tokens": 6000, "estimated_cost_usd": 0.18, "card_count": 4, "has_estimates": true }
  ],
  "card_costs_30d": [
    { "card_id": "ALPHA-003", "card_title": "...", "assigned_agent": "claude-7a3f", "prompt_tokens": 5000, "completion_tokens": 1200, "estimated_cost_usd": 0.033, "has_estimates": true }
  ],
  "chat_cost_usd_last_30d": 0.142,
  "chat_cost_usd_prior_30d": 0.098,
  "chat_cost_series_30d": [0.001, 0.004, 0.009, "... 30 daily buckets, oldest first"]
}
```

- `*_parents` variants count only top-level cards (no `parent`), so the UI
  can show run-level numbers next to card-level ones.
- `total_cost_has_estimates*` and each row's `has_estimates` are `true` when
  any contributing bucket has `cost_source: estimated`; omitted when false.
- `assigned_agent` on a `card_costs_30d` row is omitted when no agent owns
  the card. Rows fold each subtask's tokens and cost into the parent, so a
  parent row appears even when only its subtasks have spend; subtasks whose
  parent no longer exists keep their own row.
- `model_costs_30d` aggregates per model; cards with an empty `model` string
  bucket under `"unknown"`. A card with a `usage_breakdown` contributes to
  every model row it used; one without lands under `token_usage.model`.
- Both rollups cover cards whose `updated` falls inside the last 30 days, the
  same boundary as `total_cost_usd_last_30d`, so `card_costs_30d` sums to
  that figure. `total_cost_usd_prior_30d` covers days 30-60 ago.
  `cost_series_30d` buckets by `updated` day, oldest first.
- `cards_completed_last_7d` / `cards_completed_prior_7d` count cards whose
  `updated` falls inside the trailing and preceding 7-day windows.
- `metric_series` is an 8-sample daily window (oldest first, today last).
  `shipped` is bucketed by `updated` on `done` cards; the others are
  reconstructed from each card's `state_changed` activity entries.
  `active_agents` counts cards whose reconstructed end-of-day state is
  `in_progress`/`review` **and** which currently have an assigned agent
  (approximate - claim history is not tracked). Cards that pre-date
  state-change logging fall back to their current `state`.
- `chat_cost_*` fields are **server-wide** aggregates riding on the
  per-project payload. They sum `estimated_cost_usd` across all chat sessions
  bucketed by `last_active` on UTC midnight; today occupies
  `chat_cost_series_30d[29]`. The summary is cached for 30 seconds, so the
  fan-out requests the All Projects view makes return identical values.
  `chat_cost_series_30d` is omitted when empty; treat missing as `0`.

Prometheus counters related to chat cost (admin `/metrics` listener):

| Counter                                        | Labels  | Meaning                                                                                   |
| ---------------------------------------------- | ------- | ----------------------------------------------------------------------------------------- |
| `contextmatrix_chat_usage_unknown_model_total` | `model` | A chat usage frame named a model not in `token_costs`; tokens accumulate, cost stays `$0` |
| `contextmatrix_chat_cost_summary_errors_total` | -       | The chat-cost summary failed inside the dashboard; chat-cost fields render as zero        |

### GET /api/projects/{project}/activity

Chronological flat feed of activity-log entries across every card in the
project, newest first. Backfills entries older than the page load (SSE
delivers everything from page load forward).

- `limit` (optional, default 50): non-integer or `<= 0` is 400; values above
  500 clamp to 500.

```json
{
  "items": [
    { "agent": "claude-7a3f", "action": "claimed", "message": "...", "card_id": "ALPHA-003", "ts": "2026-05-17T12:34:56Z" }
  ]
}
```

`message` is omitted when empty. The feed is rolling (no cursor): clients
refresh by re-fetching.

### POST /api/projects/{project}/recalculate-costs

Admin-only in `auth.mode: multi`. Re-prices every card in the project from
the current `token_costs` rates.

```json
{ "default_model": "claude-sonnet-4-6" }
```

`default_model` is required (400 without it) and is used for cards that have
tokens but no model recorded. Cards with a `usage_breakdown`: every bucket
with `cost_source: estimated` is re-priced; `cost_source: actual` buckets are
never modified. Legacy cards without a breakdown: fill-missing-only - cards
with tokens but $0 cost get a cost; existing costs are kept.

```json
{ "cards_updated": 12, "total_cost_recalculated": 0.847 }
```

Token counts are always caller-reported. The MCP `report_usage` tool's
optional `source` field (`"self"`, the default, or `"collector"`) is recorded
as the bucket's `counts_source` and, like `cost_source`, is sticky; the two
are independent. Its `on_behalf_of` field overrides the bucket's `agent` key
while `agent_id` still has to hold the claim, so an orchestrator can attribute
a sub-agent's consumption to that sub-agent.

## Sync Endpoints

### POST /api/sync

Trigger a sync. With `?repo=<name>` only that boards repository syncs;
without it every repo that has a remote syncs. Returns the same list as `GET
/api/sync`. 503 `SYNC_DISABLED` when no repo (or the named repo) has a
remote; 400 `BAD_REQUEST` for an unknown repo name; 500 `SYNC_ERROR` when a
cycle failed.

### GET /api/sync

One status per configured boards repository, in config order (`[]` when no
syncer is wired).

```json
[
  {
    "repo": "team",
    "last_sync_time": "2026-04-05T12:00:00Z",
    "syncing": false,
    "enabled": true,
    "shared": true,
    "remote_reachable": true,
    "unpushed_commits": 0,
    "claims_at_risk": false
  },
  { "repo": "private", "last_sync_time": null, "syncing": false, "enabled": false, "shared": false, "unpushed_commits": 0, "claims_at_risk": false }
]
```

| Field                | Type                | Description                                                                                                                        |
| -------------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `repo`               | string              | The boards repository this status describes (`boards` on a single-repo instance)                                                   |
| `last_sync_time`     | RFC 3339 / `null`   | Last completed sync attempt; `null` if none                                                                                        |
| `last_sync_error`    | string, omitempty   | Error from the most recent failed sync                                                                                             |
| `syncing`            | bool                | `true` while a sync is in flight                                                                                                   |
| `enabled`            | bool                | The repo has a remote to sync with                                                                                                 |
| `shared`             | bool                | `boards[].shared` for this repo                                                                                                    |
| `remote_reachable`   | bool, omitempty     | Shared repos: whether the last network call reached the remote. Absent until the first call                                        |
| `last_remote_error`  | string, omitempty   | Shared repos: the last network failure, sanitized                                                                                  |
| `unpushed_commits`   | int                 | Commits on the local branch the remote does not have yet                                                                           |
| `resolutions`        | array, omitempty    | Shared repos: the last 100 resolver actions (`at`, `trigger`, plus the resolution fields: rule, path, card, what was overridden)   |
| `claims_at_risk`     | bool                | Shared repos: `true` once pushes have failed longer than `lease_interval`; peers stall this instance's cards after `lease_timeout` |
| `push_failing_since` | RFC 3339, omitempty | When the current push failure streak began                                                                                         |
| `hidden_projects`    | string[], omitempty | Projects this repo holds under a name an earlier repo owns; on disk and syncing but not served                                     |

## SSE Events

### GET /api/events

Server-Sent Events stream of every board mutation. `?project=<slug>` limits
the stream to that project; events with an empty `project` (sync, playbook)
pass every filter.

- The handler sends `: connected\n\n` on subscribe, then `: keepalive\n\n`
  every 30 seconds. `X-Accel-Buffering: no` is set to bypass nginx proxy
  buffering, and the connection's write deadline is cleared so the stream
  survives the server-wide `WriteTimeout`.
- Events are unnamed (`data: {json}\n\n`), so `EventSource.onmessage`
  receives all of them.
- 429 `TOO_MANY_SUBSCRIBERS` once 128 subscribers are connected. Each
  subscriber has a 64-event buffer; a slow consumer silently loses events
  (counted in the `contextmatrix_eventbus_dropped_total` metric) and should
  refetch on `sync.completed`.

```json
{
  "type": "card.state_changed",
  "project": "alpha",
  "card_id": "ALPHA-003",
  "agent": "human:alice",
  "timestamp": "2026-05-17T12:34:56Z",
  "data": { "old_state": "todo", "new_state": "in_progress" }
}
```

`agent` and `data` are omitted when empty. `agent` is `"system"` for
server-initiated changes (auto-transitions, stalls, sync diffs). Events that
originate from a shared-board pull carry `data.source: "sync"`.

| Type                  | `data`                                                                                                                                                                              | Emitted when                                                                           |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `card.created`        | `{source_system, title}` for imported cards, else none; sync diff: `{new_state}`                                                                                                    | Card created locally, or appeared via a shared-board pull                              |
| `card.updated`        | `{old_state, new_state}`, or `{action: "pushed", branch, pr_url}`, `{action: "review_attempts_incremented", review_attempts}`, `{autonomous: true}`, `{worker_status: "completed"}` | Non-state edit, `report_push`, `increment_review_attempts`, promote, worker completion |
| `card.state_changed`  | `{old_state, new_state}`                                                                                                                                                            | Transition via PUT/PATCH/MCP, parent auto-transition, or a sync diff                   |
| `card.deleted`        | none; sync diff: `{old_state}`                                                                                                                                                      | Card deleted                                                                           |
| `card.claimed`        | none                                                                                                                                                                                | Claim granted; `agent` is the claimant                                                 |
| `card.released`       | none; force-release: `{previous_agent, forced: true}`                                                                                                                               | Release or force-release                                                               |
| `card.stalled`        | `{previous_agent}`; foreign stall adds `claimed_via`                                                                                                                                | Heartbeat timeout, or a peer instance stalled this instance's claim                    |
| `card.log_added`      | `{action, message}`                                                                                                                                                                 | `add_log`                                                                              |
| `card.usage_reported` | `{prompt_tokens, completion_tokens, cache_read_tokens, cache_creation_tokens, model}`                                                                                               | `report_usage`                                                                         |
| `claim.lost`          | `{previous_agent, claimed_via, claim_epoch, source: "sync"}`                                                                                                                        | A shared-board pull moved a claim this instance held to another instance               |
| `worker.triggered`    | `{worker_status: "queued"}`                                                                                                                                                         | `POST .../run` accepted                                                                |
| `worker.started`      | `{worker_status: "running"}`                                                                                                                                                        | Backend reported `running`                                                             |
| `worker.failed`       | `{worker_status: "failed"}`                                                                                                                                                         | Backend reported `failed`                                                              |
| `worker.killed`       | `{worker_status: "killed"}`                                                                                                                                                         | `POST .../stop`                                                                        |
| `worker.parked`       | `{worker_status: "parked", reason}`                                                                                                                                                 | `report_parked`                                                                        |
| `project.created`     | none (`project` set, no `card_id`)                                                                                                                                                  |                                                                                        |
| `project.updated`     | none                                                                                                                                                                                |                                                                                        |
| `project.deleted`     | none                                                                                                                                                                                |                                                                                        |
| `playbook.created`    | `{id}` (`project` empty)                                                                                                                                                            |                                                                                        |
| `playbook.updated`    | `{id}`                                                                                                                                                                              | Any playbook or entry mutation                                                         |
| `playbook.deleted`    | `{id}`                                                                                                                                                                              |                                                                                        |
| `sync.started`        | `{trigger, repo}` (`project` empty)                                                                                                                                                 | `trigger` is `startup`, `manual`, or `periodic`                                        |
| `sync.completed`      | `{trigger, repo, changes_pulled, duration_ms}`                                                                                                                                      | Cycle finished; refetch when `changes_pulled` is true                                  |
| `sync.conflict`       | `{trigger, repo, error}` or `{trigger, repo, resolved}`                                                                                                                             | Merge conflict hit, or `resolved` resolver actions applied                             |
| `sync.error`          | `{trigger, repo, error}`                                                                                                                                                            | Cycle failed                                                                           |

A shared-board pull publishes per-card `card.created` / `card.updated` /
`card.state_changed` / `card.deleted` diffs (with `source: "sync"`) only when
the pull changed a bounded number of cards; larger pulls rely on the
`sync.completed` refetch.

## Playbook Endpoints

Cross-project ordered lists - global and shared, no per-project scoping, no
ownership gate. See [playbooks.md](playbooks.md) and
[data-model.md](data-model.md) § Playbooks for the file format and domain
rules. The subsystem is optional: it is disabled for the whole instance when
any boards repo has a project occupying its reserved `playbooks/` directory
(a `playbooks/.board.yaml` exists). When disabled, none of these routes are
registered (a plain 404, not `PLAYBOOK_NOT_FOUND`).

### GET /api/playbooks

List-view summary of every playbook, sorted by id.

```json
[
  {
    "id": "alpha-rollout",
    "title": "Alpha feature rollout",
    "boards_repo": "team",
    "complete": 1,
    "total": 3,
    "segments": ["complete", "active", "pending"],
    "projects": 2,
    "gates": [2],
    "next": { "type": "card", "project": "alpha", "card": "ALPHA-002", "title": "Wire the flag" },
    "updated_at": "2026-08-20T10:30:00Z"
  }
]
```

`segments` is one status per entry in playbook order (`complete` | `active`
| `missing` | `pending`; `active` means the card is `in_progress`);
`projects` counts the distinct projects referenced by card entries.
`gates` lists the indexes of manual entries (omitted when there are none).
`next` is the first incomplete entry - `title` is the card title (empty when
the card is missing), or the step text for a manual entry - and is omitted
once every entry is complete.
`boards_repo` is omitted on a single-repo instance.

### POST /api/playbooks

Creates a playbook. All-or-nothing: an invalid or duplicate entry rejects the
whole call and nothing is written.

```json
{
  "title": "Alpha feature rollout",
  "description": "optional free text",
  "boards_repo": "team",
  "entries": [
    { "type": "card", "project": "project-alpha", "card": "ALPHA-101", "note": "merge this one first" },
    { "type": "manual", "text": "Rebuild worker image and redeploy" }
  ]
}
```

The id is derived from `title` and never changes. `entries` is optional.
`boards_repo` (optional) picks the boards repository; the id is unique across
every repo. Response **201** with the full detail.

**Errors:**

| Status | Code                    | When                                                                  |
| ------ | ----------------------- | --------------------------------------------------------------------- |
| 400    | `BAD_REQUEST`           | `title` is empty, or unknown `boards_repo`                            |
| 409    | `PLAYBOOK_ENTRY_EXISTS` | Duplicate `{project, card}` entry                                     |
| 422    | `VALIDATION_ERROR`      | Invalid entry (unknown type, missing project/card/text, unknown card) |
| 503    | `REMOTE_UNREACHABLE`    | Shared boards: push could not be verified                             |

### GET /api/playbooks/{id}

Resolved detail: metadata plus every entry joined against the card store.

```json
{
  "id": "alpha-rollout",
  "title": "Alpha feature rollout",
  "description": "optional free text",
  "boards_repo": "team",
  "created_by": "human:alice",
  "created_at": "2026-08-20T09:00:00Z",
  "updated_at": "2026-08-20T10:30:00Z",
  "complete": 1,
  "total": 3,
  "entries": [
    {
      "id": "e1",
      "type": "card",
      "project": "project-alpha",
      "card": "ALPHA-101",
      "note": "merge this one first",
      "card_title": "Implement user auth",
      "card_state": "in_progress",
      "card_assigned_agent": "claude-7a3f",
      "complete": false
    },
    {
      "id": "e2",
      "type": "manual",
      "text": "Rebuild worker image and redeploy",
      "done": true,
      "done_by": "human:alice",
      "done_at": "2026-08-20T10:30:00Z",
      "complete": true
    }
  ]
}
```

A card entry is `complete` when the card is in a terminal state; a broken
reference renders with `missing: true` and counts in `total` but never in
`complete`. All entry fields except `id`, `type`, and `complete` are omitted
when empty.

**Errors:** 404 `PLAYBOOK_NOT_FOUND`.

### PATCH /api/playbooks/{id}

Updates `title` and/or `description`; omitted fields are unchanged.

```json
{ "title": "Alpha feature rollout (revised)" }
```

Response **200** with the full detail. **Errors:** 404
`PLAYBOOK_NOT_FOUND`, 422 `VALIDATION_ERROR` (empty title).

### DELETE /api/playbooks/{id}

Deletes the playbook file (git history preserves it). Cards are unaffected.
**204 No Content**. **Errors:** 404 `PLAYBOOK_NOT_FOUND`.

### POST /api/playbooks/{id}/entries

Appends one entry.

```json
{ "type": "card", "project": "project-beta", "card": "BETA-042", "note": "optional" }
```

```json
{ "type": "manual", "text": "Run the live runbook" }
```

Response **201** with the full detail.

| Status | Code                    | When                                                      |
| ------ | ----------------------- | --------------------------------------------------------- |
| 404    | `PLAYBOOK_NOT_FOUND`    | Unknown playbook id                                       |
| 409    | `PLAYBOOK_ENTRY_EXISTS` | Duplicate `{project, card}` entry                         |
| 422    | `VALIDATION_ERROR`      | Invalid entry (unknown type, missing field, unknown card) |

### PATCH /api/playbooks/{id}/entries/{entryId}

Patches one entry's `done`, `note`, `text`, or `position`. `done` and `text`
apply only to manual entries; `note` to both. Checking `done` stamps
`done_by`/`done_at` from the caller identity and server clock; unchecking
clears both. `position` is the entry's final index after the move: values
beyond the end clamp, negative is rejected.

```json
{ "done": true }
```

Response **200** with the full detail.

| Status | Code                       | When                                                             |
| ------ | -------------------------- | ---------------------------------------------------------------- |
| 404    | `PLAYBOOK_NOT_FOUND`       | Unknown playbook id                                              |
| 404    | `PLAYBOOK_ENTRY_NOT_FOUND` | Unknown entry id                                                 |
| 422    | `VALIDATION_ERROR`         | `done`/`text` on a card entry, empty `text`, negative `position` |

### DELETE /api/playbooks/{id}/entries/{entryId}

Removes one entry; its id is never reused. Response **200** with the full
detail (the playbook still exists). **Errors:** 404 `PLAYBOOK_NOT_FOUND`,
404 `PLAYBOOK_ENTRY_NOT_FOUND`.

**Attribution** (`created_by`, `done_by`): the same resolved identity as
cards - session in multi mode, else `X-Agent-ID`, falling back to
`human:web`.

## Worker & Backend Endpoints

The web UI's run controls plus the HMAC-signed callbacks the task and chat
backends make to CM. See [remote-execution.md](remote-execution.md) for the
webhook protocol, HMAC signing, and backend configuration, and
[running-cards.md](running-cards.md) for the operator workflow.

**HMAC-signed callbacks** (`/api/agent/*`, `/api/chat/*`, `/api/v1/*`)
require both `X-Signature-256` (`sha256=` + HMAC-SHA256 of the body, signed
with that backend's `api_key`; GETs sign an empty body) and
`X-Webhook-Timestamp`. A missing header, a bad signature, a timestamp more
than 5 minutes off, or a replayed signature is 403 `INVALID_SIGNATURE`. The
`/api/agent/*`, `/api/backend/health`, `/api/worker/logs`, and `/api/v1/*`
routes exist only when a task backend is configured; `/api/chat/*` only when
a chat backend is enabled with an `api_key`.

### POST /api/projects/{project}/cards/{id}/run

Trigger remote execution for a card. Human-only. Requires the card to be in
`todo` and a task backend configured. The `autonomous` flag is not required.

Optional JSON body:

```json
{ "interactive": true }
```

When `interactive` is `true`, the container starts in Human-in-the-Loop
(HITL) mode: the worker begins plan drafting immediately and pauses at its
built-in gates (plan approval, subtask execution decision, review) for input
delivered through `/message`. CM forces `interactive` off for autonomous
cards. Best-of-N and mob settings come from the stored card, not the body.

The run never modifies the card's automation flags: every card carries a
generated `branch_name`, and the stored `create_pr` value decides whether
the worker opens a pull request. The trigger payload carries a project-scoped
git token (`git_token`, `git_token_expires_at`) and the `llm_endpoint`.

Returns **202 Accepted** with the updated card (`worker_status: "queued"`) as
soon as the backend webhook is accepted; the container is provisioned
asynchronously.

| Status | Code                  | When                                                                                                       |
| ------ | --------------------- | ---------------------------------------------------------------------------------------------------------- |
| 403    | `HUMAN_ONLY_FIELD`    | Caller is not `human:`                                                                                     |
| 409    | `INVALID_TRANSITION`  | Card is not in `todo`                                                                                      |
| 409    | `WORKER_CONFLICT`     | Card is already `queued` or `running`                                                                      |
| 409    | `VALIDATION_ERROR`    | The project's `github_credential` binding is broken (worker status reverts, activity entry `run-rejected`) |
| 502    | `BACKEND_UNAVAILABLE` | Webhook failed                                                                                             |
| 503    | `BACKEND_DISABLED`    | No task backend configured                                                                                 |

### POST /api/projects/{project}/cards/{id}/message

Send a chat message to a container running in interactive mode. Human-only.
The running check runs before the body is read.

```json
{ "content": "Please focus on the authentication module first." }
```

Returns **202** with `{ "ok": true, "message_id": "<uuid>" }`.

| Status | Code                  | When                             |
| ------ | --------------------- | -------------------------------- |
| 409    | `WORKER_NOT_RUNNING`  | `worker_status` is not `running` |
| 413    | `CONTENT_TOO_LARGE`   | `content` exceeds 8 KiB          |
| 422    | `VALIDATION_ERROR`    | `content` is empty               |
| 502    | `BACKEND_UNAVAILABLE` | Webhook failed                   |
| 503    | `BACKEND_DISABLED`    | No task backend configured       |

### POST /api/projects/{project}/cards/{id}/promote

Promote an interactive session to autonomous. Human-only. Requires
`worker_status: "running"`. Two steps, in order:

1. Flip the card's `autonomous` flag, append a `promoted` activity entry,
   commit, and publish `card.updated` with `{autonomous: true}`. Idempotent:
   an already-autonomous card returns the current card without any webhook.
2. Send the `/promote` webhook to the task backend, which confirms the flag
   via `GET /api/v1/cards/{project}/{id}/autonomous` before telling the worker
   to re-read the card at its next gate.

Returns **202 Accepted** with the updated card.

| Status | Code                  | When                                                                                               |
| ------ | --------------------- | -------------------------------------------------------------------------------------------------- |
| 403    | `HUMAN_ONLY_FIELD`    | Caller is not `human:`                                                                             |
| 409    | `WORKER_NOT_RUNNING`  | `worker_status` is not `running`                                                                   |
| 409    | `INVALID_TRANSITION`  | Card is `done` or `not_planned`; nothing is sent                                                   |
| 502    | `BACKEND_UNAVAILABLE` | Webhook failed - CM rolls back the flag flip and records a `promote-webhook-failed` activity entry |

```bash
curl -X POST http://localhost:8080/api/projects/my-project/cards/PROJ-042/promote \
  -H 'X-Requested-With: contextmatrix' -H 'X-Agent-ID: human:alice'
```

### POST /api/projects/{project}/cards/{id}/stop

Stop a running execution. Human-only. Sends a kill webhook and returns
**202 Accepted** with the updated card (`worker_status: "killed"`).

**Errors:** 403 `AGENT_MISMATCH` when a peer instance holds the claim; 409
`WORKER_NOT_RUNNING` unless the status is `queued` or `running`; 502
`BACKEND_UNAVAILABLE`; 503 `BACKEND_DISABLED`.

### POST /api/projects/{project}/stop-all

Stop every queued or running execution in a project. Human-only. Cards
claimed by another instance are skipped.

```json
{ "affected_cards": ["PROJ-001", "PROJ-003"], "failed_to_update": ["PROJ-007"] }
```

**200** when every stop landed, **207 Multi-Status** when `affected_cards`
and `failed_to_update` are both non-empty. `failed_to_update` is omitted when
empty.

### GET /api/backend/health

Browser-facing fixed path (session required in multi mode). Proxies `GET
/health` on the task backend for the board's capacity meter; `max_concurrent`
is the backend-global cap.

```json
{ "ok": true, "running_containers": 2, "max_concurrent": 4 }
```

- 503 `BACKEND_DISABLED` when no task backend is configured.
- 502 `BACKEND_UNAVAILABLE` when the probe fails (timeout, transport error,
  non-2xx). Upstream details are logged, not returned; callers should hide
  the meter.

Results (including failures) are cached for 2 seconds and concurrent callers
are coalesced through singleflight; the probe uses a 3 s timeout.

### GET /api/backends/{backend}/images

Proxies `GET /images` on the named backend (`agent` or `chat`) for the
project-settings image pickers. Session-gated and admin-gated in multi mode;
open in none mode.

```json
{
  "ok": true,
  "images": [
    {
      "tags": ["ghcr.io/example-org/contextmatrix-agent-worker:2026-07-01"],
      "digests": ["ghcr.io/example-org/contextmatrix-agent-worker@sha256:abc123..."],
      "created": 1751328000,
      "size": 1073741824
    }
  ]
}
```

`digests`, `created`, and `size` are omitted when empty.

- 404 `BACKEND_NOT_FOUND` when `{backend}` is not `agent` or `chat`.
- 503 `BACKEND_DISABLED` when that backend is not configured.
- 502 `BACKEND_UNAVAILABLE` when the probe fails (5 s timeout).

Results are cached per backend for 30 seconds behind a singleflight. The
cache is load-bearing: concurrent same-second signed GETs would otherwise
collide in the backend's HMAC replay cache.

### GET /api/worker/logs

SSE log stream. Browser-facing fixed path: a session is required in multi
mode; HMAC signing toward the backend happens server-side.

| Parameter | Required | Description                                            |
| --------- | -------- | ------------------------------------------------------ |
| `project` | yes      | 400 when missing, 404 `PROJECT_NOT_FOUND` when unknown |
| `card_id` | no       | Enables card-scoped mode                               |

- **Card-scoped** (`?project=P&card_id=X`): replays every buffered event of
  the card's session (including HITL questions), then tails live events. The
  session exists from when the card enters `running` until `failed`,
  `killed`, or `completed`.
- **Project-scoped** (`?project=P`): same replay-then-tail guarantee over the
  project's buffered events. Used by the Worker Console panel.

Both return **204** when no session manager is wired. The response is
`text/event-stream` with `X-Accel-Buffering: no`; `: keepalive\n\n` is
written every 30 seconds.

Each normal event carries a JSON payload:

```json
{
  "ts": "2026-04-08T12:34:56.789Z",
  "card_id": "PROJ-042",
  "type": "text",
  "content": "[round 1] seat-1 (correctness): the parser change misses...",
  "seq": 42,
  "agent": "seat-1",
  "model": "z-ai/glm-5.2"
}
```

`type` is passed through from the backend's log entry: `text`, `thinking`,
`tool_call`, `stderr`, `system`, `user`, `usage` (empty `content`), or
`status`. `agent` and `model` appear only on mob-session discussion frames
(`seat-1`..`seat-N`, `guest-<name>`, `moderator`, `human`; see
[remote-execution.md](remote-execution.md#mob-sessions)).

Marker frames:

| Frame type | Payload                                | Meaning                                                    |
| ---------- | -------------------------------------- | ---------------------------------------------------------- |
| `terminal` | `{"type":"terminal","seq":N}`          | Session ended; no further events                           |
| `dropped`  | `{"type":"dropped","seq":N,"count":N}` | Server ring-buffer overflowed; `count` events were evicted |

The web client tracks `seq`, renders a gap marker when `seq > lastSeq + 1`
or on `dropped`, and stops reconnecting after `terminal`.

### POST /api/agent/status

Task-backend callback reporting a worker-status transition.

```json
{ "card_id": "PROJ-042", "project": "my-project", "worker_status": "running", "message": "container started" }
```

`worker_status` must be `running`, `failed`, or `completed`; the server-only
`queued` and `killed` are 422 `VALIDATION_ERROR`. `message`, when present,
is appended to the card's activity log. Returns **200** with the updated
card; 400 `BAD_REQUEST` on malformed JSON.

### GET /api/v1/cards/{project}/{id}/autonomous

Task-backend read used by `/promote`'s fail-closed check. Returns exactly
`{"autonomous": <bool>}`; no other card field is exposed on this path.

### GET /api/agent/task-skills-source

Returns the task-skills repo pointer the backend clones for itself. `GET
/api/chat/task-skills-source` returns the same shape for the chat backend.

```json
{
  "git_remote_url": "https://github.com/org/task-skills.git",
  "ref": "9fceb02d0ae598e95dc970b74767f19372d61af8",
  "token": "ghs_...",
  "token_expires_at": "2026-07-05T13:00:00Z"
}
```

`git_remote_url` and `ref` come from `task_skills.dir`: when the directory is
a git checkout, `git_remote_url` is its `origin` URL and `ref` is the resolved
HEAD commit SHA. When it is not a checkout, `git_remote_url` falls back to the
configured `task_skills.git_remote_url` and `ref` is `""`. Both are `""` when
neither is available.

`token` is a clone credential minted from the instance `github.*` credential
(the task-skills repo is instance-scoped, never bound to a project pool
entry). It is best-effort: when minting fails the response carries only the
pointer and the backend falls back to its own credential. `token_expires_at`
is absent for PAT-backed credentials (no server-managed TTL; absent means
"do not schedule a refresh").

### GET /api/agent/git-credentials

Re-mints the project-scoped git token mid-run - App installation tokens live
about an hour while card runs can go far longer.

Query parameters `project` and `card_id` are both required (400). The card
must exist and have `worker_status: running`, so the endpoint is not a free
token faucet. The token is minted from the project's `github_credential`
binding (or the instance credential when unbound); a broken binding fails
closed and never substitutes the instance credential.

```json
{ "token": "ghs_...", "expires_at": "2026-07-05T13:00:00Z" }
```

`expires_at` is absent for PAT-backed credentials.

| Status | Code               | When                                       |
| ------ | ------------------ | ------------------------------------------ |
| 409    | `VALIDATION_ERROR` | Card not running, or the binding is broken |
| 502    | `INTERNAL_ERROR`   | A provider resolved but minting failed     |

## Worker Endpoints

### GET /api/worker/git-credentials

Serves per-repo git credentials to chat workers on demand. Authentication is
a **bearer token**, not HMAC request signing and not the session cookie:

```
Authorization: Bearer <session_id>.<base64url HMAC-SHA256 mac>
```

The token is minted once per chat session at chat-start
(`git_credentials_token` in the chat-start payload) from the chat backend's
`api_key` and the session id. It is deterministic and never persisted:
verification re-derives it from the session id embedded in the token and
compares in constant time. The model is identical in both auth modes.

| Param  | Meaning                                                                     |
| ------ | --------------------------------------------------------------------------- |
| `host` | Bare host of the repo the worker is about to operate on (e.g. `github.com`) |
| `path` | `owner/repo`, with or without a trailing `.git`                             |

Resolution: `(host, path)` is matched case-insensitively (host ports, trailing
dots, and `.git` suffixes normalized) against every project's `repo` or
`repos`; only an exact `owner/repo` match selects a project. A matched
project resolves through its `github_credential` binding (fail-closed on a
broken binding; an unbound project uses the instance credential). No match
resolves to the instance-wide `github.*` credential. Both parameters omitted
(the shape a repo-less `gh` call sends) skips matching and also resolves to
the instance credential; exactly one present is 400.

```json
{ "username": "x-access-token", "token": "ghs_...", "expires_at": "2026-07-05T13:00:00Z" }
```

`username` is always the literal `x-access-token`. `expires_at` is omitted
for PAT-backed credentials.

| Status | Code                 | When                                                                     |
| ------ | -------------------- | ------------------------------------------------------------------------ |
| 400    | `BAD_REQUEST`        | Exactly one of `host`/`path` is missing                                  |
| 401    | `UNAUTHORIZED`       | Bearer absent, malformed, or fails the HMAC comparison                   |
| 404    | `CHAT_NOT_FOUND`     | The session id embedded in the bearer does not exist                     |
| 409    | `WORKER_NOT_RUNNING` | The session exists but is cold                                           |
| 409    | `VALIDATION_ERROR`   | Matched project's binding is broken, or no provider is configured at all |
| 500    | `INTERNAL_ERROR`     | Session-liveness lookup failed for another reason                        |
| 502    | `INTERNAL_ERROR`     | A provider resolved but minting failed                                   |

Neither the bearer nor any minted token is ever logged or echoed in
`details`. The route is registered only when a chat backend with an
`api_key` and the chat manager are both wired; GET-only, so the CSRF guard
does not apply.

## Chat Endpoints

Project-agnostic chat sessions that run the same worker image as card runs
in long-lived containers. Routes are registered only when the chat manager
and SSE hub are wired. Identity follows the `X-Agent-ID` convention; the
server defaults to `human:web` when the header is absent.

**Ownership (multi mode):** every session is owned by the identity that
created it (`created_by`, `human:<username>`). The list is force-scoped to
the caller (`?created_by=` is ignored), and every per-ID endpoint returns the
same `404 CHAT_NOT_FOUND` for foreign and nonexistent IDs. In `none` mode
the surface is unscoped and `?created_by=` is a plain filter. Admin
management lives under `/api/admin/chats`.

### Session object

| Field                                                                              | Presence  | Meaning                                                                                    |
| ---------------------------------------------------------------------------------- | --------- | ------------------------------------------------------------------------------------------ |
| `id`, `title`, `status`, `created_at`, `last_active`, `created_by`                 | always    | `id` is a 26-character base32 ULID; `status` is `cold`, `active`, `warm-idle`, or `ending` |
| `project`, `container_id`, `workspace`, `model`                                    | omitempty | `workspace` is a string list; `model` is set at creation and reused on every `/open`       |
| `context_tokens`                                                                   | omitempty | Last `input + cache_read + cache_create` reported by the model                             |
| `context_tokens_updated_at`                                                        | always    | Zero time (`0001-01-01T00:00:00Z`) until the first usage entry                             |
| `rehydration_active`, `rehydration_started_at`                                     | omitempty | `true` between a cold reopen and the agent's `chat_rehydration_complete` call              |
| `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens` | omitempty | Cumulative totals across usage frames                                                      |
| `estimated_cost_usd`                                                               | omitempty | Running cost, same cache-tier formula as card-scoped `report_usage`                        |
| `assistant_working`, `assistant_working_since`                                     | omitempty | In-memory presence derived from `status` log frames                                        |

### POST /api/chats

Creates a session row in `cold`; no container starts.

```json
{ "title": "Investigate auth-flow regression", "project": "contextmatrix", "model": "claude-opus-4-8" }
```

All three fields are optional, but the body must be valid JSON (`{}` at
minimum). An empty `title` is filled from the first user message; `project`
may be empty for cross-project chats; `model` defaults to
`backends.chat.default_model` and is forwarded to the container on every
`/open`.

Model validation follows the active source (see `GET /api/chats/models`):
in `endpoint` mode `model` must be in the endpoint's served list, in
`openrouter` mode in CM's vendor-screened catalog; either way an unknown slug
is 400 `INVALID_MODEL`, and an unfetched or failed list fails open. When both
an OpenAI-compatible endpoint and an OpenRouter chat backend are configured,
endpoint mode wins. With neither, the value is accepted unvalidated.

**Response:** 201 with the session.

### GET /api/chats/models

Tells the New Chat dialog which model picker to render.

```json
{
  "source": "openrouter",
  "models": [ { "id": "anthropic/claude-sonnet-4.5", "label": "anthropic/claude-sonnet-4.5", "max_tokens": 200000 } ],
  "default": "anthropic/claude-sonnet-4"
}
```

- `"openrouter"`: `models` is the vendor-screened OpenRouter catalog (`id` =
  `label` = slug, `max_tokens` = context window) from the server-side cache;
  empty only when the catalog has not been fetched.
- `"endpoint"`: `llm_endpoint.type: openai`. `models` is the endpoint's
  served list, cached for 5 minutes with last-good fallback; when the
  upstream fetch fails the response carries `fetch_error` with an empty
  list. Also returned as `{"source":"endpoint","models":[],"default":""}`
  when no chat backend is configured.

Blacklisted models carry `"blacklisted": true` (absent otherwise, best-effort
join, informational only - see `GET /api/models`).

### GET /api/chats

List sessions, newest first by `last_active`. Always an array, `[]` when
empty.

| Param        | Default | Max  | Effect                                                             |
| ------------ | ------- | ---- | ------------------------------------------------------------------ |
| `project`    | -       | -    | Filter by project name                                             |
| `status`     | -       | -    | `cold` / `active` / `warm-idle` / `ending`; unknown values are 400 |
| `created_by` | -       | -    | Filter by agent ID (`none` mode only)                              |
| `limit`      | `500`   | 5000 | Above the max clamps; non-numeric or `< 1` is 400                  |

### GET /api/chats/{id}

Returns the session. `404 CHAT_NOT_FOUND` if unknown.

### PATCH /api/chats/{id}

Renames a session. Body is decoded before the ownership check, so bad JSON
is 400 even for an unknown id.

```json
{ "title": "Renamed: auth-flow regression" }
```

**Response:** 200 with the updated session.

### DELETE /api/chats/{id}

Removes the session and its transcript. An `active` or `warm-idle` container
is ended first; live streams are closed. **204 No Content**.

### POST /api/chats/{id}/open

Starts the chat container for a `cold` session. Idempotent for `active`;
reattaches for `warm-idle`. **200** with the refreshed session (`status:
active`, `container_id` set). 429 `TOO_MANY_CHATS` when `chat.max_concurrent`
(active plus warm-idle plus in-flight opens; `0` = unlimited) is reached.

### POST /api/chats/{id}/end

Closes the container's stdin and force-stops it. **200** with the refreshed
session (`status: cold`, `container_id` cleared). Already-cold is a no-op.

### POST /api/chats/{id}/clear

Clears the worker's working memory without ending the session. The server
sends `/clear` to the worker, marks every prior transcript row
`rehydration_phase = true` (excluded from future cold-open resume payloads),
and appends a divider row (`role: system`, `content: "Context cleared"`,
`kind: "divider"`) that is both broadcast and persisted. The body is ignored.

| Status | Code                  | Meaning                                                                     |
| ------ | --------------------- | --------------------------------------------------------------------------- |
| 202    | -                     | Cleared; body `{"ok": true}`                                                |
| 404    | `CHAT_NOT_FOUND`      | Unknown session id                                                          |
| 409    | `WORKER_NOT_RUNNING`  | Session is not `active` or `warm-idle`                                      |
| 502    | `BACKEND_UNAVAILABLE` | `/clear` send failed (`details: clear_failed`); transcript untouched, retry |
| 500    | `INTERNAL_ERROR`      | Worker cleared but the transcript mark/divider failed                       |

### POST /api/chats/{id}/messages

Sends a user message. The content checks run before the ownership check. The
message goes to the chat backend first; only on success is the row appended
with a server-assigned `seq` and broadcast as a `user` event. A `cold`
session is opened automatically (so 429 `TOO_MANY_CHATS` is possible);
`warm-idle` is promoted to `active`.

```json
{ "content": "Show me the diff between v1 and v2 of the auth middleware." }
```

**Response:** 202 with `{ "ok": true, "message_id": "<26-char id>" }`.
400 `BAD_REQUEST` for empty or whitespace `content`; 413 `CONTENT_TOO_LARGE`
above 8 KiB.

### GET /api/chats/{id}/messages

Transcript pages over the persisted rows, always in ascending `seq` order.
Three keyset modes, mutually exclusive by presence (more than one is 400):

| Param        | Default | Max  | Effect                                                |
| ------------ | ------- | ---- | ----------------------------------------------------- |
| `since_seq`  | `0`     | -    | Forward: rows with `seq > N`                          |
| `tail=1`     | -       | -    | Newest `limit` rows - the lazy-history bootstrap page |
| `before_seq` | -       | -    | Backward: newest `limit` rows with `seq < N`          |
| `limit`      | `200`   | 1000 | Above the max clamps; `<= 0` or non-numeric is 400    |

`tail=1` exists because JavaScript cannot represent `before_seq=2^63-1`.
Seqs run `1..N` with no holes, so history remains below the loaded window
exactly while the oldest loaded seq is `> 1`.

```json
{
  "messages": [
    { "id": 1, "session_id": "01J...", "seq": 1, "role": "user", "content": "hi", "created_at": "2026-05-14T12:00:00Z" },
    { "id": 2, "session_id": "01J...", "seq": 2, "role": "assistant_text", "content": "hello", "created_at": "2026-05-14T12:00:01Z" }
  ]
}
```

`role` is `user`, `assistant_text`, `assistant_thinking`, `tool_call`,
`tool_result`, `stderr`, or `system`. `kind` (`"divider"`) and
`rehydration_phase` appear only when set. `content` is the raw text.
Empty transcripts return `{"messages": []}`; unknown session is 404.

The browser fetches the newest page with `tail=1`, subscribes to
`/stream?since_seq=<last>` so the seam is gapless, and pages older history
with `before_seq=<oldest loaded>`, deduplicating on `seq`.

### GET /api/chats/{id}/stream

SSE stream of new transcript entries for one session. `?since_seq=N` replays
entries with `seq > N` from the server-side 128-entry ring buffer before
tailing (an unparseable value is treated as `0`). The handler flushes `:
connected\n\n` on subscribe and `: keepalive\n\n` every 15 seconds. 404
`CHAT_NOT_FOUND` for an unknown session; 429 `TOO_MANY_CHATS` once 32
subscribers are attached to one session. Slow subscribers silently drop
events.

Two event kinds share the wire:

- **Transcript event** - unnamed, so `EventSource.onmessage` fires:

  ```json
  { "seq": 7, "role": "assistant_text", "content": "...", "kind": "divider", "rehydration_phase": true }
  ```

  `kind` and `rehydration_phase` are omitted when unset.

- **`session_updated`** - `event: session_updated`, never replayed from the
  ring buffer. The payload is a partial session to merge into the local view:

  | Field                                                                                                    | Description                                                   |
  | -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
  | `context_tokens`, `context_tokens_updated_at`                                                            | Updated context-window count; the timestamp is always present |
  | `model`                                                                                                  | Set on the first usage event                                  |
  | `rehydration_active`                                                                                     | `false` once `chat_rehydration_complete` is called            |
  | `status`                                                                                                 | Present only when the lifecycle state changed; never `ending` |
  | `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `estimated_cost_usd` | New cumulative totals; omitted when zero                      |
  | `assistant_working`, `assistant_working_since`                                                           | Presence derived from `status` log frames                     |

  Status transitions that emit `status`: `cold → active` on `/open` or the
  first message; `warm-idle → active` when a browser subscriber attaches or
  `/open` is called; `active → warm-idle` 30 s after the last subscriber
  leaves; `active/warm-idle → cold` on `/end` or the idle reaper
  (`chat.idle_ttl`). Clients should refetch `GET /api/chats` on a `status`
  change.

## Image Endpoints

Backs the paste / drag-drop upload flow in the card editor and the inline
image attachments on the MCP `get_card` / `get_task_context` tools. IDs are
the first 16 hex chars of `sha256(processed_bytes)`, so identical uploads
dedup and URLs are stable. An upload that names a project in a shared boards
repo is stored as `<project>/images/<id>.<ext>` in that repo, committed and
pushed like a card; every other upload (no `project`, an unknown project, a
private repo) goes to SQLite (`images.db`, configurable via `images.db_path`
/ `CONTEXTMATRIX_IMAGES_DB_PATH`). Reads consult the repos first and the
database second.

### POST /api/images

Multipart form upload: `file` (required) and `project` (optional; the web
client always sends it). The route's body cap is 11 MB (10 MB image plus
multipart headroom) instead of the global 5 MB.

Processing:

- Accepts `image/png`, `image/jpeg`, `image/gif` (single-frame), `image/webp`.
- Multi-frame GIF: 415 `IMAGE_ANIMATED` (detected by a pre-decode header
  walk). Animated WebP: 415 `IMAGE_UNSUPPORTED`, because the `x/image/webp`
  decoder is still-only. Anything else: 415 `IMAGE_UNSUPPORTED`.
- Over 10 MB: 413 `CONTENT_TOO_LARGE`.
- Resizes to fit within 1024x768 (CatmullRom), re-encodes in the same format
  (PNG, or JPEG q85); GIF and WebP become PNG. The re-encode strips EXIF.

```bash
curl -X POST http://localhost:8080/api/images \
  -H 'X-Requested-With: contextmatrix' \
  -H 'X-Agent-ID: human:alice' \
  -F 'file=@screenshot.png' \
  -F 'project=alpha'
```

Response 201:

```json
{ "id": "aabbccddeeff0011", "url": "/api/images/aabbccddeeff0011" }
```

Error codes: `CONTENT_TOO_LARGE` (413), `IMAGE_UNSUPPORTED` /
`IMAGE_ANIMATED` (415), `IMAGE_MISSING_FILE` / `IMAGE_INVALID_PAYLOAD` (400).

### GET /api/images/{id}

Serves the stored blob with its `Content-Type`, `Content-Length`, and
`Cache-Control: public, max-age=31536000, immutable`. 404 `IMAGE_NOT_FOUND`
for an unknown or malformed id.

## MCP Endpoints

The MCP server is mounted at `/mcp` for `POST`, `GET`, and `DELETE` per the
Streamable HTTP transport, on the same middleware chain as every REST route.
The tool catalogue, connection instructions, and workflow entry points are in
[mcp.md](mcp.md); this section covers only the wire-level rules.

- **Auth:** when `mcp_api_key` is set, every request needs `Authorization:
  Bearer <key>`; any failure is 401 with `WWW-Authenticate: Bearer
  realm="contextmatrix"` and the constant body `{"error":"unauthorized"}`.
  With no key the endpoint is unauthenticated. `/mcp` is exempt from the
  CSRF guard and the session guard.
- **`X-CM-Chat-Session`:** chat containers forward their session id in this
  header; it must match `^[A-Z2-7]{26}$` or the request is 400. The value
  gates session-scoped tools (`chat_rehydration_complete`). Absent for
  card-mode workers and humans.
- **Identity:** every tool takes an `agent_id` argument; there is no session
  concept.

### Card payload shapes: full vs summary

Only `get_card` and `get_task_context` (primary card and parent) return the
full card. Every other card-bearing tool returns a **card summary** - the
same JSON as the [Card object](#card-object) minus `body`, `activity_log`,
and `usage_breakdown`, the three unbounded fields. Mutation results are
re-read by the calling agent on every subsequent model call, so echoing
those fields would multiply context cost for no information gain.

| Shape                                              | Tools                                                                                                                                                                                                                                                                         |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Full card                                          | `get_card`; `get_task_context` (primary card and parent)                                                                                                                                                                                                                      |
| Card summary                                       | `create_card`, `update_card`, `transition_card`, `claim_card`, `release_card`, `add_log`, `complete_task`, `report_usage`, `report_push`, `report_parked`, `promote_to_autonomous`, `increment_review_attempts`, `list_cards`, `get_ready_tasks`, `get_task_context` siblings |
| Minimal ack (`card_id`, `state`, `last_heartbeat`) | `heartbeat`                                                                                                                                                                                                                                                                   |

`token_usage` (the cumulative total) is in summaries, so budget sync never
needs the per-bucket breakdown. Siblings are summaries because they exist
for overlap awareness (title, state, labels, `depends_on`); subtask detail
flows through per-card `get_card` fetches. A structural consequence:
unvetted external card bodies cannot leak through any mutation or list
result. Body redaction for non-human callers applies to the surfaces that
carry bodies: `get_card`, `get_task_context`, and the skill-injection path
(redaction runs before section filtering).

#### `update_card` autonomous flag

`update_card` accepts an optional `autonomous` boolean. Any MCP-connected
agent can set it - the intended path for a harness planning multiple tasks
to mark which are suitable for autonomous execution. Omitting the field
leaves the stored value unchanged; setting it only writes the flag and never
triggers a running worker (use `promote_to_autonomous`). The REST surfaces
keep `autonomous` human-only. See [data-model.md](data-model.md) §
`autonomous`.

#### `update_card` depends_on

`update_card` accepts an optional `depends_on` array that replaces the full
list; omitted leaves it unchanged, `[]` clears. `PATCH
/api/projects/{project}/cards/{id}` has identical semantics. Both reject an
unknown ID, a cross-project ID, a self-reference, or a cycle with
`DEPENDENCIES_NOT_MET`.

#### `update_card` section upsert

`update_card` accepts an `upsert_section_heading` + `upsert_section_content`
pair (both or neither) to replace or append a single `## <heading>` section
without resending the body. Mutually exclusive with `body` in the same call.
Replace-or-append is keyed on an exact, flush-left heading match. A repeat
call with the same content leaves the body unchanged but still advances
`updated` and records a commit. The resulting body is capped at 512 KB.
MCP-only; no REST equivalent. Full semantics: [data-model.md](data-model.md)
§ Section upsert.

#### Skill-injection body filtering

`get_skill`, `start_review`, and `start_workflow` inject card context into
the skill content they return. The late-run surfaces filter the injected
body to the sections the skill consumes; the pre-heading intro is always
kept, and a bracketed note names any omitted sections with a pointer to
`get_card`:

| Skill surface                                                                                                   | Injected body                                                          |
| --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `review-task` (`start_review`, `get_skill`)                                                                     | intro + `## Plan` + `## Review Findings` (all rounds) + `## Decisions` |
| `document-task` (`get_skill`)                                                                                   | intro + `## Plan` + `## Decisions`                                     |
| `execute-task` parent card                                                                                      | intro + `## Plan`                                                      |
| `create-plan`, `plan-draft`, `brainstorming`, `systematic-debugging`, `run-autonomous`, `execute-task` own card | full body                                                              |

Heading matching is case-insensitive and tolerates a round suffix
(`## Review Findings (Round 2)` matches). When none of a filter's sections
exist in a body, the full body passes through unchanged - the failure
direction is over-injection, never omission.

#### `get_card` payload opt-ins

Two independent opt-ins, composable with each other and with
`include_images`:

- `include_activity_log: false` drops `activity_log` (default `true`).
- `sections: ["Plan", ...]` returns only the named `## <heading>` sections;
  the pseudo-entry `"intro"` includes the pre-heading text. Matching uses the
  same rule as the skill-injection filter, but the fallback is reversed: a
  request that matches nothing returns `body: ""`, not the full body. The
  image scan runs against the filtered body.

### `get_card` / `get_task_context` - inline image attachments

Both tools scan the primary card body for `![](/api/images/<16-hex>)`
references (relative or absolute against this server) and attach the bytes
as MCP `ImageContent` blocks alongside the JSON `TextContent` block.

- Capped at 10 images and about 20 MiB of raw bytes per call, in body order;
  later references are kept in the body but not inlined, and truncation is
  logged with the tool name and card id.
- Unknown IDs are silently skipped.
- `include_images: false` opts out.
- `get_task_context` scans the primary card only; siblings stay text-only.

### `chat_rehydration_complete`

Marks the active chat session's rehydration phase complete and emits the
final summary message. Called by a chat-mode worker after ingesting the
rehydration prompt.

- `session_id` (string, required)
- `summary` (string, required) - surfaced to the UI as an assistant message

The caller's `X-CM-Chat-Session` header must equal `session_id`, otherwise
the call is rejected. A caller with no header (card-mode or out-of-band) is
allowed, but `session_id` must still resolve to an active chat session.
