# REST API Reference

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

POST   /api/projects/{project}/cards/{id}/claim      # agent identity from X-Agent-ID header
POST   /api/projects/{project}/cards/{id}/release     # agent identity from X-Agent-ID header
# heartbeat, log, context, usage, and report-push have no REST endpoint — the
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
GET    /api/agent/task-skills-source                   # task-skills {git_remote_url, ref} pointer (HMAC-signed; also /api/chat/... for the chat backend)
GET    /api/agent/git-credentials  ?project=&card_id=  # mid-run project git-token refresh (HMAC-signed; task-backend required)
GET    /api/backend/health                             # proxied backend /health (capacity meter; 2s cached; fixed path)
GET    /api/backends/{backend}/images                  # proxied backend /images (worker-image picker; agent|chat; 30s cached; admin-gated in multi mode)
GET    /api/worker/logs  ?project=&card_id=            # SSE log stream (card-scoped or project-scoped; fixed path; task-backend required)
GET    /api/v1/cards/{project}/{id}/autonomous         # backend autonomous-flag read (HMAC-signed; task-backend required)
# /api/agent/* — task-backend callback path; /api/chat/* — chat-backend callback path (paths are fixed)

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
GET    /api/chats/{id}/messages                        ?since_seq=&limit=    # transcript bootstrap
GET    /api/chats/{id}/stream                          ?since_seq=           # SSE stream of new entries

POST   /api/sync                                      # trigger git sync
GET    /api/sync                                       # sync status

GET    /api/task-skills                                # list available task skill names
GET    /api/app/config                                 # server-side app config (theme/palette/version/auth_mode; slim pre-login payload in multi mode)

POST   /api/auth/login                                  # session-cookie login (multi mode only)
POST   /api/auth/logout                                 # clear session (multi mode only)
GET    /api/auth/session                                # who am I (multi mode only; 401 if not logged in)
GET    /api/auth/token/{token}                          # inspect a bootstrap/invite/reset token (multi mode only)
POST   /api/auth/token/{token}                          # redeem token: set password + auto-login (multi mode only)
POST   /api/auth/password                                # change own password (multi mode only)

GET    /api/admin/users                                 # list accounts (admin-only, multi mode only)
POST   /api/admin/users                                 # create account + invite link (admin-only, multi mode only)
PATCH  /api/admin/users/{username}                       # update display_name/is_admin/disabled (admin-only, multi mode only)
POST   /api/admin/users/{username}/invite                 # regenerate invite/reset link (admin-only, multi mode only)
GET    /api/admin/credentials                            # list GitHub credential pool metadata (admin-only, multi mode only)
POST   /api/admin/credentials                            # add a pool credential (admin-only, multi mode only)
PUT    /api/admin/credentials/{name}                      # rotate secret / update metadata / disable (admin-only, multi mode only)
DELETE /api/admin/credentials/{name}                      # remove a pool credential (admin-only, multi mode only)

GET    /api/admin/model-outcomes                            # Best-of-N per-model outcome stats (both auth modes; admin-gated only in multi)
DELETE /api/admin/model-outcomes                            # reset recorded outcomes (both auth modes; admin-gated only in multi)

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

**Agent identification:** `X-Agent-ID` header supplies agent identity. It is
required on the agent endpoints (`/claim`, `/release`) and on any mutation of
a claimed card — there the header value must match `assigned_agent` (403 on
mismatch). It also gates human-only fields and human-only endpoints (`/run`,
`/stop`, `/message`, `/promote`, `/stop-all`): those require an `X-Agent-ID`
value beginning with `human:`. Read endpoints, project CRUD, sync, branches,
app config, task-skills, healthz, and readyz do not require the header.
Request bodies on agent endpoints do not carry an `agent_id` field; it is
silently ignored if present.

In `auth.mode: multi`, a logged-in session's identity (`human:<username>`)
takes precedence over `X-Agent-ID` on any request that carries a valid session
cookie; the header is consulted only when there is no session (MCP, HMAC
backend callbacks, or `auth.mode: none`). This upgrades the claim/release
ownership check above from a courtesy into real enforcement — see §
Authentication (multi mode) below.

**Identity is a tag, not auth (`auth.mode: none`).** In `none` mode
ContextMatrix is single-tenant with no auth layer below `X-Agent-ID`; spoofing
it accomplishes nothing because there is no permission gradient to escalate
into. The `human:` prefix gates workflow contracts (only humans promote), not
security boundaries — true in both modes, since MCP never gained a session
concept. The web UI generates a per-browser identity (`human:web-<8 hex
chars>`) and never prompts the operator for a username. Routes that act on
behalf of the web UI fall back to `human:web` or `human:api` when no header is
present — intentional, because the UI is the only legitimate caller. In
`multi` mode these fallbacks are unreachable dead code on session-gated
routes: the session guard has already rejected any request with no session
before the fallback would run.
See § Trust model in `CLAUDE.md` and in `docs/architecture.md`.

**CSRF protection:** every state-changing request on the main listener must
carry `X-Requested-With: contextmatrix`. The web UI sets this header on every
non-GET fetch in `web/src/api/client.ts`. Cross-origin browsers cannot set
custom headers on a "simple request" without a CORS preflight, and the server
serves no permissive CORS for state-changing routes — a missing header is
therefore a strong cross-origin signal and the request is rejected with 403
`BAD_REQUEST`. Exempt paths:

- `GET` / `HEAD` / `OPTIONS` on any route (read-only).
- `/api/agent/*`, `/api/chat/*` — backend callback paths, authenticated via
  per-backend HMAC; not browser paths.
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
  `/stop-all`, `/api/agent/status`,
  `POST /api/chats/{id}/open`, `POST /api/chats/{id}/end`,
  `GET /api/v1/cards/.../autonomous`, `DELETE /api/admin/model-outcomes` —
  the latter returns the deleted row count rather than an empty 204 body)
- 201: created (`POST /api/projects`, `POST /api/projects/{p}/cards`,
  `POST /api/chats`, `POST /api/admin/users`, `POST /api/admin/credentials`)
- 202: accepted — async endpoint kicked off background work (`POST /run`,
  `/stop`, `/message`, `/promote`, chat `/messages`)
- 204: deleted (DELETE); also `POST /api/auth/logout`,
  `POST /api/auth/password`, `DELETE /api/admin/credentials/{name}`
- 400: malformed input (bad JSON, missing/bad query param, unknown filter value,
  missing CSRF header) — emitted with code `BAD_REQUEST`
- 401: no/expired session, multi mode only (`UNAUTHORIZED`) — the SPA
  redirects to the login page; also a missing/invalid bearer on
  `GET /api/worker/git-credentials`, independent of `auth.mode`
- 403: agent mismatch (wrong agent trying to modify claimed card), unvetted card
  claim attempt (`CARD_NOT_VETTED`), agent attempting a human-only field
  mutation (`HUMAN_ONLY_FIELD`), HMAC signature / timestamp invalid on a
  backend-callback endpoint (`INVALID_SIGNATURE`), authenticated but not admin on
  an admin-gated route, multi mode only (`FORBIDDEN`)
- 404: card, project, chat session, or referenced parent not found —
  parent-not-found uses code `PARENT_NOT_FOUND`; also unknown one-time token
  or unknown admin username (`TOKEN_INVALID`, `USER_NOT_FOUND`)
- 409: conflict (invalid transition, card already claimed, already-running
  worker task → `WORKER_CONFLICT`); also a bootstrap token redeemed after a
  user already exists, or an edit that would leave zero active admins
  (`VALIDATION_ERROR`); also a cold chat session or a broken/unavailable git
  credential on `GET /api/worker/git-credentials`
  (`WORKER_NOT_RUNNING` / `VALIDATION_ERROR`)
- 410: one-time token already redeemed or past its 48-hour expiry
  (`TOKEN_INVALID`)
- 413: request body / chat message exceeds the size cap (`CONTENT_TOO_LARGE`)
- 422: semantic validation error — mutation body references an unknown type,
  state, priority, or invalid autonomous combination. Emitted with code
  `VALIDATION_ERROR`. **Not** used for 400-class failures.
- 429: concurrent chat cap reached (`TOO_MANY_CHATS`), or too many failed
  login attempts for one account+IP (`RATE_LIMITED`, `Retry-After` header set)
- 502: backend unreachable (`BACKEND_UNAVAILABLE`); also a git-credential
  mint failure on `GET /api/agent/git-credentials` or
  `GET /api/worker/git-credentials` (`INTERNAL_ERROR`)
- 503: no task backend configured (`BACKEND_DISABLED`), sync disabled
  (`SYNC_DISABLED`), or `/readyz` dependency check failed

**Error code / HTTP status mapping (selected):**

| Code                      | HTTP    | Meaning                                                       |
| ------------------------- | ------- | ------------------------------------------------------------- |
| `BAD_REQUEST`             | 400     | malformed input / unknown filter value / CSRF missing         |
| `UNAUTHORIZED`            | 401     | no/expired session (multi mode); SPA redirects to login       |
| `FORBIDDEN`               | 403     | authenticated but not admin, on an admin-gated route (multi mode) |
| `RATE_LIMITED`            | 429     | too many failed logins for one account+IP; `Retry-After` header set |
| `TOKEN_INVALID`           | 404/410 | one-time token unknown (404), or already redeemed/expired (410) |
| `USER_NOT_FOUND`          | 404     | unknown username on an admin user-management route            |
| `PROJECT_NOT_FOUND`       | 404     | project slug does not exist                                   |
| `CARD_NOT_FOUND`          | 404     | card ID does not exist in the project                         |
| `PARENT_NOT_FOUND`        | 404     | referenced parent card does not exist                         |
| `CHAT_NOT_FOUND`          | 404     | chat session ID does not exist                                |
| `VALIDATION_ERROR`        | 422     | mutation body semantically invalid                            |
| `INVALID_MODEL`           | 400     | chat `model` not in the active model source (endpoint list, or CM's vendor-screened OpenRouter catalog) |
| `WORKER_CONFLICT`         | 409     | card already queued/running                                   |
| `BACKEND_DISABLED`        | 503/403 | no task backend configured globally (503) or disabled for the project (403) |
| `BACKEND_UNAVAILABLE`     | 502     | backend webhook failed (host unreachable)                     |
| `WORKER_NOT_RUNNING`      | 409     | card is not currently running; also a cold (non-live) chat session on `GET /api/worker/git-credentials` |
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
| `HUMAN_ONLY_FIELD` | 403  | An agent without `human:` prefix attempts to set `autonomous`, `feature_branch`, `create_pr`, `vetted`, `base_branch`, a model pin (`model_orchestrator`, `model_coder`, `model_reviewer`), `best_of_n`, a mob field (`mob_participants`, `mob_phases`, `mob_guests`), or `verify`. |

## Authentication (multi mode)

`/api/auth/*` and `/api/admin/*` are registered only when `auth.mode: multi`
(the config default — see the `auth` block in `config.yaml.example`). In
`auth.mode: none`, `RouterConfig.AuthService` is nil and `NewRouter` skips
these routes, the admin routes, and the `sessionGuard` middleware entirely —
the router is byte-for-byte identical to a build with no auth concept, and
every endpoint documented in this section simply is not registered: a plain
`404 page not found` (no JSON body), not `401`. See § Trust model in
`docs/architecture.md` for the full security-review framing; this section
documents the wire contract.

**Exception:** `GET`/`DELETE /api/admin/model-outcomes` is the one
`/api/admin/*` pair registered in **both** auth modes — Best-of-N outcome
tracking does not depend on the auth system. It is documented at the end of
this section, after the chat admin routes.

**Session gate.** `sessionGuard` runs on every request in multi mode and
rejects any request with no valid session — reads as well as writes, unlike
none mode's write-only agent-identity checks. Exempt paths (reachable without
a session): `/healthz`, `/readyz`, `/mcp`, `/api/auth/*` itself,
`/api/app/config` (serves a slim pre-login payload — see below), the
HMAC-signed backend-callback prefixes `/api/agent/*`, `/api/chat/*`,
`/api/v1/*`, and `/api/worker/*` (bearer-authed, not HMAC — see § Worker
Endpoints below). The browser-facing `/api/worker/logs` and
`/api/backend/health` are not backend-callback paths and do require a session.
`/api/auth/*` being session-exempt at the
router level does not mean unauthenticated — `GET /api/auth/session` and
`POST /api/auth/password` check the session themselves and 401 without one.

**Admin gate.** `requireAdmin` layers on top of the session gate. It guards
every `/api/admin/*` route below, plus four project-management REST call
sites: `POST /api/projects`, `PUT /api/projects/{project}`,
`DELETE /api/projects/{project}`, and
`POST /api/projects/{project}/recalculate-costs` — and, separately,
`GET /api/backends/{backend}/images` (the worker-image picker source).
Ordinary card work — claim, release, update, transition, activity — needs
only a valid session, any role.

**The 401 / 403 contract:**

| Status | Code           | Meaning                                                                        |
| ------ | -------------- | ------------------------------------------------------------------------------- |
| 401    | `UNAUTHORIZED` | No session cookie, or the session is invalid/expired/for a disabled user. The SPA redirects to the login page. |
| 403    | `FORBIDDEN`    | A valid session exists but the user is not an admin, on an admin-gated route. |

Neither code is returned by `auth.mode: none` — there is no session concept,
so the code paths that produce them do not run.

**Session cookie:** `cm_session` — `HttpOnly`, `SameSite=Lax` always, `Secure`
when the request arrived over TLS (directly or via `X-Forwarded-Proto`). The
value is a random 256-bit token; only its SHA-256 hash is persisted
server-side, so a stolen `auth.db` yields no usable session. A session idle
past 5 minutes gets a sliding renewal to `now + auth.session_idle_ttl`
(default 720h / 30 days) on its next validated request, and the response
re-sets the cookie with the refreshed `Max-Age`.

**CSRF still applies.** `/api/auth/*` and `/api/admin/*` are ordinary
state-changing routes — they are **not** in the CSRF-exempt path list (see §
CSRF protection above). Every non-GET call, including `POST /api/auth/login`
itself, needs `X-Requested-With: contextmatrix` or it is rejected with 403
`BAD_REQUEST` before the session/credentials are even checked.

The token-authority endpoints (`GET /api/agent/task-skills-source`,
`GET /api/agent/git-credentials`) are a separate, HMAC-signed machine
channel documented under § Worker & Backend Endpoints below — they are unrelated to the
session/admin system here and exist in both auth modes. `GET
/api/worker/git-credentials` (§ Worker Endpoints below) is a further machine
channel with its own auth — a deterministic per-session bearer token, not
HMAC request signing — likewise unrelated to sessions/admin and present in
both modes.

### POST /api/auth/login

```json
{ "username": "alice", "password": "correct-horse-battery" }
```

On success, sets the `cm_session` cookie and returns **200** with the session
identity:

```json
{ "username": "alice", "display_name": "Alice Nakamura", "is_admin": true }
```

Failures are deliberately uniform: an unknown username, a disabled account, a
wrong password, and an account whose invite was never redeemed (no password
set) all return the same **401 `UNAUTHORIZED`** ("invalid credentials") — the
response never discloses whether a username exists.

Repeated failures for the same (normalized username, client IP) pair trip an
in-memory rate limiter: the first two failures are free, the third blocks for
1 second, doubling per further failure up to a 5-minute cap. A blocked
attempt returns **429 `RATE_LIMITED`** with a `Retry-After` header (seconds).

The client IP is the TCP peer address (`RemoteAddr`); `X-Forwarded-For` is
deliberately not consulted, since honoring it without a trusted-proxy
allowlist would let any client spoof its limiter key. Behind a reverse proxy
all logins therefore share the proxy's IP in limiter keys — the per-account
half still applies. A trusted-proxy knob is recorded as future work.

```bash
curl -i -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: contextmatrix' \
  -d '{"username":"alice","password":"correct-horse-battery"}'
```

### POST /api/auth/logout

No request body — deletes the session behind the `cm_session` cookie (if any)
and clears the cookie. Idempotent: logging out with no session, or an
already-expired one, still returns **204 No Content**.

### GET /api/auth/session

"Who am I." Router-exempt from the session gate (so the login page can probe
it without a redirect loop), but the handler enforces auth itself: **401
`UNAUTHORIZED`** with no valid session, otherwise **200** with the same shape
as the login response above.

### GET /api/auth/token/{token}

Inspects a one-time token (bootstrap / invite / password-reset link)
**without consuming it** — the redemption page uses this to render the right
form before the user submits anything.

```json
{ "purpose": "invite", "username": "bob" }
```

`purpose` is `bootstrap`, `invite`, or `reset`. `username` is omitted for
bootstrap tokens (the account does not exist yet).

**Errors:**

| Status | Code            | When                                                    |
| ------ | --------------- | ---------------------------------------------------------- |
| 404    | `TOKEN_INVALID` | Unknown token                                               |
| 410    | `TOKEN_INVALID` | Token already redeemed                                      |
| 410    | `TOKEN_INVALID` | Token past its 48-hour expiry (`auth.OneTimeTokenTTL`)      |

### POST /api/auth/token/{token}

Redeems the token: sets a password and logs the user in.

```json
{ "username": "bob", "display_name": "Bob Okafor", "password": "correct-horse-battery" }
```

`username` / `display_name` apply only to a `bootstrap` token, where they
create the first admin account (`is_admin: true` unconditionally — bootstrap
is refused once any user exists). For `invite` / `reset` tokens only
`password` is read; redeeming a `reset` token additionally kills every other
live session for the account.

On success: sets the `cm_session` cookie, returns **200** with the session
identity (same shape as login). Validation runs **before** the token is
consumed, so a rejected form does not burn the link.

**Errors:**

| Status | Code               | When                                                                     |
| ------ | ------------------ | ----------------------------------------------------------------------- |
| 400    | `BAD_REQUEST`       | Malformed JSON body                                                     |
| 404    | `TOKEN_INVALID`     | Unknown token                                                           |
| 409    | `VALIDATION_ERROR`  | Bootstrap token redeemed after a user already exists                    |
| 410    | `TOKEN_INVALID`     | Token already redeemed or expired                                      |
| 422    | `VALIDATION_ERROR`  | `password` under 10 characters                                          |
| 422    | `VALIDATION_ERROR`  | (bootstrap only) username invalid — "1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation" |
| 422    | `VALIDATION_ERROR`  | (bootstrap only) username already taken                                |

### POST /api/auth/password

Self-service password change for the logged-in caller. Router-exempt from the
session gate like `/session` above; the handler enforces auth itself.

```json
{ "current_password": "correct-horse-battery", "new_password": "another-correct-horse" }
```

Verifies `current_password`, sets `new_password`, and deletes every other
session for the account — the session making this call survives. Returns
**204 No Content**.

**Errors:**

| Status | Code               | When                                          |
| ------ | ------------------ | ------------------------------------------------ |
| 401    | `UNAUTHORIZED`     | No session, or `current_password` is wrong        |
| 422    | `VALIDATION_ERROR` | `new_password` under 10 characters                |

**Admin endpoints.** Every route below additionally requires an admin session
(**403 `FORBIDDEN`** otherwise — see § Admin gate above).

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

The server does not know its own public address, so it returns the raw token
rather than a composed link; the admin UI builds the shareable
`/auth/token/<token>` frontend route from it. The token is valid for 48 hours
(`auth.OneTimeTokenTTL`) — use `POST /api/admin/users/{username}/invite` to
mint a replacement if it expires or is lost.

**Errors:** `422 VALIDATION_ERROR` — username already taken, or invalid
("1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation").

### PATCH /api/admin/users/{username}

Partial update — only keys present in the body are applied, in the order
`display_name`, `is_admin`, `disabled`:

```json
{ "display_name": "Bob O.", "is_admin": true, "disabled": false }
```

Demoting (`is_admin: false`) or disabling (`disabled: true`) the last active
admin is refused before any field in the body is applied, so a mid-patch
refusal never leaves earlier fields written. Disabling a user immediately
deletes their sessions (they are logged out mid-flight); re-enabling does not
restore them. Returns **200** with the updated account (same shape as the
list above).

**Errors:**

| Status | Code               | When                              |
| ------ | ------------------ | ------------------------------------ |
| 404    | `USER_NOT_FOUND`   | Unknown username                     |
| 409    | `VALIDATION_ERROR` | Would leave zero active admins       |

### POST /api/admin/users/{username}/invite

Mints a fresh one-time link, invalidating any earlier unused link of the same
purpose. Purpose is chosen automatically: `invite` if the account has never
had a password, `reset` otherwise.

```json
{ "token": "raw-one-time-token", "purpose": "reset", "expires_at": "2026-07-05T12:00:00Z" }
```

**Errors:** `404 USER_NOT_FOUND` for an unknown username.

### GET /api/admin/credentials

Lists the GitHub credential pool — metadata only; encrypted secrets never
leave the server.

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
actually resolved for a token. A project binds to one pool entry via its
`.board.yaml` `github_credential` field — see `docs/data-model.md`.

### POST /api/admin/credentials

Validates the credential's shape, checks it live against GitHub (an `app`
entry mints an installation token; a `pat` entry probes `/rate_limit`),
encrypts the secret, and stores it. A typo'd credential fails here, not days
later inside an agent run.

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
no leading/trailing punctuation. Response **201** with the same shape as the
list above.

**Errors:**

| Status | Code               | When                                                                       |
| ------ | ------------------ | --------------------------------------------------------------------------- |
| 422    | `VALIDATION_ERROR` | Bad shape (name, empty secret, `app` entry missing `app_id`/`installation_id`, key does not parse) |
| 422    | `VALIDATION_ERROR` | Credential rejected by the live GitHub check                                |
| 422    | `VALIDATION_ERROR` | `name` already taken                                                        |
| 500    | `INTERNAL_ERROR`   | Auth master key not configured                                              |

### PUT /api/admin/credentials/{name}

Rotate the secret, update metadata, and/or toggle `disabled` — any subset in
one call, applied in that order (metadata, then secret, then disabled):

```json
{ "secret": "ghp_...", "host": "", "api_base_url": "", "disabled": false }
```

A metadata change (`host` / `api_base_url` / `app_id` / `installation_id`)
re-validates against GitHub using the **currently stored** secret merged with
the new metadata, so a host change that would silently break the credential
is caught here instead of on the next run. Rotating the secret independently
re-validates the new secret against the stored metadata. Returns **200** with
the updated credential.

**Errors:**

| Status | Code               | When                                                           |
| ------ | ------------------ | -------------------------------------------------------------- |
| 422    | `VALIDATION_ERROR` | Empty `secret` on rotate, or GitHub rejected the credential    |
| 404    | `VALIDATION_ERROR` | Unknown credential `name`                                      |
| 500    | `INTERNAL_ERROR`   | Auth master key not configured                                 |

### DELETE /api/admin/credentials/{name}

Refuses to delete a credential that any project's `.board.yaml`
`github_credential` still references:

```json
{ "error": "credential is bound to projects", "code": "VALIDATION_ERROR", "details": "rebind first: alpha, beta" }
```

**409** in that case — rebind the listed projects first. Otherwise **204 No
Content**, or **404 `VALIDATION_ERROR`** for an unknown `name` (same
not-found convention as `PUT` above).

### GET /api/admin/chats

Lists every chat session on the instance — no owner scoping. Metadata
and cost totals only; transcript content is never included. Query
parameters `project`, `status`, and `limit` (default 500, max 5000)
mirror `GET /api/chats`; `created_by` is not part of the admin filter
surface.

**Response:** `200 OK` — array of session objects (same shape as
`GET /api/chats`).

### POST /api/admin/chats/{id}/end

Force-ends any session regardless of owner — the remedy when a stuck
active session holds a slot of the global concurrency cap. Same
semantics and error mapping as `POST /api/chats/{id}/end`; returns the
updated session.

**Errors:** `404 CHAT_NOT_FOUND` unknown ID · `403 FORBIDDEN` non-admin.

### DELETE /api/admin/chats/{id}

Deletes any session regardless of owner. Unknown IDs return 404 — the
session is loaded before deletion (existing manager semantics). Cost
tombstones survive, so dashboard aggregates stay accurate.

**Response:** `204 No Content` · `404 CHAT_NOT_FOUND` unknown ID.

There is deliberately no admin route that returns transcript content
(no messages, stream, or open) — admin chat management is metadata and
lifecycle only.

### GET /api/admin/model-outcomes

Unlike every route above, this pair is registered in **both** auth modes —
Best-of-N outcome tracking does not depend on the auth system. In
`auth.mode: none` it is open (same trust posture as project management); in
`auth.mode: multi` it requires an admin session, same as the routes above
(**403 `FORBIDDEN`** otherwise — see § Admin gate above).

Returns aggregated per-model head-to-head stats recorded by the Best-of-N
judge phase (agent backend only; see `docs/remote-execution.md`), as reported
by the MCP `report_model_outcome` tool:

```json
{
  "outcome_floor": 20,
  "total_samples": 84,
  "models": [
    {
      "model": "deepseek/deepseek-v4-flash",
      "samples": 22,
      "wins": 13,
      "win_rate": 0.59,
      "expected_wins": 9.5,
      "total_cost_usd": 1.42,
      "active": true
    }
  ]
}
```

`outcome_floor` echoes the configured `best_of_n.outcome_floor`
(`config.yaml`). `active` is `true` once a model's `samples` reaches that
floor — below it, `win_rate` is too thin a sample to bias selection, and the
agent's registry ignores the entry. `win_rate` is `0` when `samples` is `0`.

### DELETE /api/admin/model-outcomes

Deletes every recorded outcome row. Returns **200 OK** (not 204 — the body
reports what was deleted) with the row count:

```json
{ "deleted": 84 }
```

Same both-modes-registered / admin-gated-in-multi behavior as `GET` above.

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
| `session_log` | always reports `ok: true`. A nil session-log manager simply means no task backend is configured (still healthy); a non-nil manager means it is operational. The check is included for forward compatibility but never fails the probe today. |

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

Returns the server-configured application settings. Exempt from the session
guard in multi mode — the SPA needs `theme` and `auth_mode` before a session
exists — but an unauthenticated caller in multi mode gets a slimmer payload
(see below). Called by the frontend on startup to determine which color
palette to apply and whether to route to the login page.

**Response — `none` mode (always), or an authenticated caller in `multi`
mode:**

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
  "chat_enabled": true
}
```

`theme` is one of `"everforest"` (default), `"radix"`, or `"catppuccin"`. The
frontend sets `data-palette` on `<html>` to match the theme value;
`"everforest"` removes the attribute (it is the default CSS block). `version` is
the build version string the binary was compiled with; it is always present and
is empty when the binary is built without the version ldflag. `auth_mode` is
`"multi"` or `"none"`, matching the configured `auth.mode`. `task_backend`
is the active task-backend name (`"agent"`, or `""` when none is
configured). `favorites` maps each tier to its operator-configured preferred
model slugs (the leaderboard "favorites as pin presets" surface); the field is
omitted entirely when no favorites are configured. `best_of_n_max` /
`best_of_n_default` mirror `config.yaml`'s `best_of_n.max_candidates` /
`best_of_n.default_candidates` — the card panel's Best-of-N selector uses them
as its upper bound and recommended value. Both are effectively always present (the
configured values default to 5 and 3 and are never valid below 2), but like
`favorites` they ride only on the full payload.
`mob_max_participants` / `mob_default_participants` mirror `config.yaml`'s
`mob.max_participants` / `mob.default_participants` and bound the card
panel's mob session seats selector. `mob_guest_names` lists the `mob.guests`
registry names for the guest multi-select — names only, never URLs or
tokens; it is omitted when the registry is empty. All three ride only on the
full payload, like the `best_of_n` fields.
`chat_enabled` is true when a chat backend is configured (an enabled
`backends.chat` entry with `url` and `api_key` set — the same condition the
per-backend images route uses for its chat probe client) — the settings UI
uses it to decide whether to render the chat image picker. Unlike the other
UI-facing fields above, it has no `omitempty`: it is always present on the
full payload, `true` or `false`, and simply absent from the slim pre-login
payload below.

**Response — unauthenticated caller in `multi` mode:** `task_backend`,
`favorites`, `best_of_n_max`, `best_of_n_default`, `mob_max_participants`,
`mob_default_participants`, `mob_guest_names`, and `chat_enabled` are not
disclosed pre-login, so they are absent from the JSON entirely (not sent as
zero values):

```json
{ "theme": "everforest", "version": "v0.42.0", "auth_mode": "multi" }
```

```bash
curl http://localhost:8080/api/app/config
# → {"theme":"everforest","version":"v0.42.0","auth_mode":"none","task_backend":"agent","best_of_n_max":5,"best_of_n_default":3}
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

Admin-only in `auth.mode: multi` (403 `FORBIDDEN` for a non-admin session) —
see § Authentication (multi mode) above. Create a new project. Either `name`
(slug) or `display_name` (human-readable) must be provided; both may be
provided together.

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

Admin-only in `auth.mode: multi` (403 `FORBIDDEN` for a non-admin session) —
see § Authentication (multi mode) above.

Update the project configuration. The update body is a **subset** of
`POST /api/projects` — `name`, `display_name`, and `prefix` are immutable and
not accepted here. Extra fields are available: `github` (GitHub import
configuration), `default_skills` (project-wide task-skill fallback),
`github_credential` (credential-pool binding for this project's GitHub
operations), `verify` (project-wide verify gate), and `remote_execution`
(per-project execution toggle and worker images).

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
  "default_skills": ["go-development", "documentation"],
  "github_credential": "org-app",
  "verify": { "command": "make test", "timeout_seconds": 600, "env": ["JAVA_HOME"] },
  "remote_execution": {
    "enabled": true,
    "worker_image": "my-org/go-worker:latest",
    "chat_worker_image": "my-org/go-chat-worker:latest"
  }
}
```

To rename a project or change its prefix, recreate it via `POST /api/projects`.

**`github_credential` field** — binds this project's GitHub operations
(branch listing, issue import) to one instance credential-pool entry (see
`GET /api/admin/credentials` above). Full validation rules and both-mode
behavior are documented in `docs/data-model.md`'s `github_credential` section
— not repeated here.

**`default_skills` field** — three-state semantics for the project-wide
task-skill fallback:

| Value                                 | Meaning                                                           |
| ------------------------------------- | ----------------------------------------------------------------- |
| field omitted / `null`                | Clear: the backend mounts the full task-skills set from `task_skills.dir` |
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

**`verify` field** — replace-whole-struct semantics: omitting the object
preserves the stored config; a present object replaces it, and a zero-value
object clears it. The server validates the config (invalid → 422
`VALIDATION_ERROR`) and normalizes a zero value to absent. Fields and limits are
documented in `docs/data-model.md`'s `verify` section. The same object is
accepted on card create (`POST .../cards`) and card PATCH — both human-only (an
agent setting `verify` gets 403 `HUMAN_ONLY_FIELD`) — where the card value
overrides the project's field by field at trigger time.

**`remote_execution` field** — per-field pointer-merge semantics, unlike
`verify`'s replace-whole-struct: each of `enabled`, `worker_image`, and
`chat_worker_image` is independently omittable (preserves the stored value) or
explicitly set. For the two image fields, an explicit empty string clears the
override back to that backend's own default image. `worker_image` feeds the
task backend's runs only; `chat_worker_image` feeds chat sessions only —
neither field falls back to the other. Both are hygiene-validated (charset,
512-byte cap) — invalid values return 422 `VALIDATION_ERROR`. Full field
semantics are documented in `docs/data-model.md`'s remote-execution section.

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

Admin-only in `auth.mode: multi` (403 `FORBIDDEN` for a non-admin session) —
see § Authentication (multi mode) above. Recalculate estimated costs for all
cards in a project using current `token_costs` rates. Requires
`default_model` for cards that have tokens but no model recorded.

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

## Worker & Backend Endpoints

The web UI's run controls plus the HMAC-signed callbacks the task and chat
backends make to CM. See [`docs/remote-execution.md`](remote-execution.md) for
the full webhook protocol, HMAC signing details, and backend configuration.

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
mode: the worker begins plan drafting immediately and pauses at its built-in
gates (plan approval, subtask execution decision, review), waiting on human
input delivered through the chat input. CM forces `interactive` off for
autonomous cards server-side.

Regardless of `interactive`, `feature_branch` and `create_pr` are automatically
enabled on the card for all run triggers (both autonomous and HITL).

Returns **202 Accepted** with the updated card (`worker_status: "queued"`). The
response is returned as soon as the backend webhook is accepted — the backend
then provisions the container asynchronously.

### POST /api/projects/{project}/cards/{id}/message

Send a chat message to a container running in interactive mode. Human-only.
Requires `worker_status: "running"`.

```json
{ "content": "Please focus on the authentication module first." }
```

- 422 if `content` is empty
- 413 `CONTENT_TOO_LARGE` if `content` exceeds 8 KiB
- 409 `WORKER_NOT_RUNNING` if the card is not running

Returns 202 with:

```json
{ "ok": true, "message_id": "uuid-v4-string" }
```

### POST /api/projects/{project}/cards/{id}/promote

Promote an interactive session to autonomous mode. Human-only. Requires
`worker_status: "running"`.

The endpoint performs two steps in order:

1. Calls `CardService.PromoteToAutonomous` to flip the card's `autonomous` flag
   to `true`, append an activity log entry, commit the change to the boards git
   repository, and publish a `CardUpdated` SSE event. This step is
   **idempotent** — if the card is already autonomous, it short-circuits and
   returns the current card without sending any webhook (preventing the
   backend's verify from recursing).
2. Sends a `/promote` webhook to the task backend, which fail-closed confirms
   the flag via `GET /api/v1/cards/{project}/{id}/autonomous` before writing a
   canned stdin message telling the worker to re-read the card at its next gate
   and continue on the autonomous branch.

`feature_branch` and `create_pr` are also set to `true` if not already enabled.

**Error responses:**

- 403 `HUMAN_ONLY_FIELD` if the caller is not a human agent
- 409 `WORKER_NOT_RUNNING` if `worker_status` is not `"running"`
- 409 `INVALID_TRANSITION` if the card is in a terminal state (`done` or
  `not_planned`) — the flag flip itself is rejected before any webhook is sent
- 502 `BACKEND_UNAVAILABLE` if the backend webhook fails — CM rolls back the
  `autonomous`, `feature_branch`, and `create_pr` changes it made so the card's
  declared mode matches the worker's actual mode, and records a
  `promote-webhook-failed` activity entry

Returns **202 Accepted** with the updated card. The idempotent short-circuit
(card already autonomous) also returns 202 with the current card state and no
new log entry.

```bash
curl -X POST http://localhost:8080/api/projects/my-project/cards/PROJ-042/promote \
  -H "X-Agent-ID: human:alice"
```

### POST /api/projects/{project}/cards/{id}/stop

Stop a running remote execution. Human-only. Sends a kill webhook to the task
backend. Returns **202 Accepted** with the updated card (`worker_status:
"killed"`).

### POST /api/projects/{project}/stop-all

Stop all running remote executions in a project. Human-only. Returns
`{ "affected_cards": ["PROJ-001", "PROJ-003"] }`.

### GET /api/backend/health

Browser-facing fixed path. Proxies a `GET /health` to the configured task
backend and returns the parsed shape. Used by the board's capacity meter
(`max_concurrent` is the backend-global cap, not a per-project value).

Returns:

```json
{
  "ok": true,
  "running_containers": 2,
  "max_concurrent": 4
}
```

- 503 `BACKEND_DISABLED` when no task backend is configured.
- 502 `BACKEND_UNAVAILABLE` when a task backend is configured but the probe
  fails (timeout, transport error, non-2xx response). Upstream error
  details are not surfaced in the response body — the underlying error
  is logged server-side. Callers should fail soft (hide capacity).

Probe results are cached server-side for ~2 seconds and concurrent callers are
coalesced through singleflight, so many open tabs and a backend outage never
storm the backend with redundant probes. Probes use a tighter upstream timeout
(3 s) than the backend client's default to keep the endpoint responsive during
an outage.

### GET /api/backends/{backend}/images

Proxies a `GET /images` to the named backend and returns the worker images
present on its node — feeds the project-settings image pickers. `{backend}` is
`agent` or `chat`.

In multi mode this route requires a session (like the rest of the API) and is
additionally admin-gated inside the handler; in none mode it is open, like the
rest of the API.

Returns:

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

`digests`, `created`, and `size` are omitted when zero/empty.

- 404 `BACKEND_NOT_FOUND` when `{backend}` is not `agent` or `chat`.
- 503 `BACKEND_DISABLED` when that backend is not configured.
- 502 `BACKEND_UNAVAILABLE` when the backend is configured but the probe fails.

Probe results are cached server-side per backend for 30 seconds behind a
singleflight — longer than the health cache, and load-bearing: concurrent
same-second signed GETs would otherwise collide in the backend's HMAC replay
cache.

### GET /api/worker/logs

SSE log stream. Browser-facing fixed path. Only available when a task backend
is configured. Not authenticated — the browser connects directly; HMAC signing
is performed server-side toward the backend.

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
  card-scoped path. Used by the Worker Console panel. A reconnecting client
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
  "content": "[round 1] seat-1 (correctness): the parser change misses...",
  "seq": 42,
  "agent": "seat-1"
}
```

Marker frames have a distinct shape:

| Frame type | Payload shape                          | Meaning                                                    |
| ---------- | -------------------------------------- | ---------------------------------------------------------- |
| `terminal` | `{"type":"terminal","seq":N}`          | Session ended; no further events                           |
| `dropped`  | `{"type":"dropped","seq":N,"count":N}` | Server ring-buffer overflowed; `count` events were evicted |

`type` for normal events is one of: `text`, `thinking`, `tool_call`,
`stderr`, `system`, `user`.

`agent` is present only on mob session discussion frames and carries the
speaker attribution (`seat-1`..`seat-N`, `guest-<name>`, `moderator`,
`human`); ordinary frames omit the key. See
[`docs/remote-execution.md` § Mob sessions](remote-execution.md#mob-sessions).

The connection is closed when the browser disconnects or the session receives a
terminal event.

**Client behaviour (`useWorkerLogs`):**

- Tracks last-seen `seq`; if an incoming `seq > lastSeq + 1`, inserts a
  client-side gap marker (`type: 'gap'`) indicating the number of missing
  events.
- `dropped` frames render as gap markers (not as ordinary log lines).
- `terminal` frames clear `connected` and stop the reconnect loop — no further
  reconnect is attempted after a clean session end.

See [`docs/remote-execution.md`](remote-execution.md) for the full log streaming
architecture, `LogEntry` type details, and session manager configuration.

### POST /api/agent/status

Task-backend callback reporting a card's worker-status transition. Mounts at the
fixed task-backend callback path `/api/agent/status`. Requires **both** an
`X-Signature-256` header (HMAC-SHA256, prefixed with `sha256=`, signed with the
backend's `api_key`) and an `X-Webhook-Timestamp` header (used for clock-skew
rejection). Missing either header, a malformed signature, or an expired
timestamp returns 403 `INVALID_SIGNATURE`. Only registered when a task backend
is configured.

Accepts `worker_status` updates (`"running"`, `"failed"`, `"completed"`,
validated by `board.ValidateWorkerCallbackStatus`). The server-only statuses
`"queued"` and `"killed"` are rejected with 422 `VALIDATION_ERROR`.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "worker_status": "running",
  "message": "container started"
}
```

### GET /api/v1/cards/{project}/{id}/autonomous

Task-backend read endpoint (fixed path, independent of the callback path).
Authenticated with HMAC-SHA256 over an empty body (`X-Signature-256` +
`X-Webhook-Timestamp`, signed with the task backend's `api_key`). Returns the
minimal shape `{"autonomous": <bool>}` so the backend can fail-closed verify a
card's autonomous flag during `/promote` before writing the canned stdin
message. Only registered when a task backend is configured. No other card
fields are exposed on this path.

### GET /api/agent/task-skills-source

Task-backend callback at the fixed path `/api/agent/task-skills-source`.
Authenticated with HMAC-SHA256 over an empty body (`X-Signature-256` +
`X-Webhook-Timestamp`, signed with the task backend's `api_key`), the same
scheme as `/autonomous`. Returns the task-skills repo pointer the backend clones
for itself:

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
and have `worker_status: running` — a non-running card gets 409, so the
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
way; a broken binding rejects the run with 409, reverts the card's worker
status, and appends a card activity entry.

## Worker Endpoints

### GET /api/worker/git-credentials

Serves per-repo git credentials to chat workers on demand. Unlike every
other backend-callback endpoint in this document, authentication is a
**bearer token**, not HMAC-SHA256 request signing and not the browser
session cookie:

```
Authorization: Bearer <session_id>.<base64url HMAC-SHA256 mac>
```

The token is minted once per chat session at chat-start
(`ChatStartPayload.git_credentials_token`) from the chat backend's
`backends.chat` `api_key` and the session id. It is deterministic: CM never
persists it anywhere, so verification just re-derives the expected token
from the same `api_key` plus the session id embedded in the presented
token, and compares with `hmac.Equal` (constant-time — a timing
side-channel cannot help an attacker guess a valid token byte-by-byte).
This auth model is identical in `auth.mode: none` and `auth.mode: multi`:
the bearer is independent of sessions, of `X-Agent-ID`, and of HMAC request
signing.

Query parameters (required as a pair — see below for the one exception):

| Param  | Meaning                                                       |
| ------ | -------------------------------------------------------------- |
| `host` | Bare host of the repo the worker is about to operate on (e.g. `github.com`) |
| `path` | `owner/repo`, with or without a trailing `.git`                |

Resolution: `(host, path)` is matched — case-insensitively, `.git` suffix
tolerated on either side — against every project's effective repo(s)
(`.board.yaml` `repo` or `repos`; a project with neither field set, or an
unparseable repo URL, is skipped). Only an **exact** owner/repo match
selects a project — a request path that is merely a prefix/superset of a
project's repo path never matches. A match resolves the credential via that
project's `github_credential` binding, **fail-closed**: a broken binding
rejects with 409 and never substitutes the instance credential. No match
resolves directly to the instance-wide `github.*` credential — the correct
credential for a non-project repo, not a degraded fallback. An empty pair
(both `host` and `path` omitted — the shape a repo-less `gh` call sends,
e.g. `gh repo create`, `gh api /user`, run from a cwd with no origin remote)
skips project matching entirely and also resolves to the instance credential;
exactly one of the two present without the other is still a 400.

Success response:

```json
{ "username": "x-access-token", "token": "ghs_...", "expires_at": "2026-07-05T13:00:00Z" }
```

`username` is always the literal `x-access-token` (the Basic-auth username a
GitHub App/PAT-backed HTTPS clone expects alongside the token). `expires_at`
is RFC3339 and omitted for PAT-backed credentials — same omission semantics
as `GET /api/agent/git-credentials` above (no server-managed TTL; absent
means "do not schedule a refresh").

Status contract:

| Status | Code                 | When                                                                                                             |
| ------ | -------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 400    | `BAD_REQUEST`        | Exactly one of `host`/`path` is missing (both missing is not an error — see above) |
| 401    | `UNAUTHORIZED`       | Bearer header absent, malformed, or fails the HMAC comparison                                                   |
| 404    | `CHAT_NOT_FOUND`     | The session id embedded in the bearer does not exist                                                            |
| 500    | `INTERNAL_ERROR`     | Session-liveness lookup failed for a reason other than "session not found" (backend/store error)                |
| 409    | `WORKER_NOT_RUNNING` | The session exists but is cold (no live worker container)                                                       |
| 409    | `VALIDATION_ERROR`   | Matched project's credential binding is broken, or no provider is configured at all — fail-closed, never the instance credential |
| 502    | `INTERNAL_ERROR`     | A credential provider resolved, but minting the token itself failed                                             |

**Secrets hygiene:** the bearer token and every minted git token are opaque
to logging — neither is ever written to a log line or an
`APIError.details` field (`sanitizeErrorDetails` still runs over provider
errors here, same as every other credential-minting endpoint, so transport
errors don't leak filesystem paths or hostnames either).

Registered only when a chat backend is configured with a non-empty
`api_key` **and** the chat manager is wired — otherwise the route is not
registered at all (a plain 404, not 401), the same posture as every other
backend-callback route in this document. Because the endpoint is `GET`-only,
it is exempt from the CSRF guard via the "GET/HEAD/OPTIONS" rule (§ CSRF
protection above); no separate path exemption was needed.

## Chat Endpoints

Project-agnostic chat sessions that run the same worker image as card runs but
use long-lived containers instead of card-scoped one-shots. Identity follows the
same `X-Agent-ID` tagging convention as the rest of the API (see § Trust model
in `CLAUDE.md`); the web UI defaults to `human:web` when the header is absent.

**Ownership (multi mode):** every chat session is owned by the identity
that created it (`created_by`, `human:<username>`). The list is
force-scoped to the caller (`?created_by=` is ignored), and every per-ID
endpoint returns an identical `404 CHAT_NOT_FOUND` for foreign and
nonexistent IDs — existence is not leaked. (`DELETE` of a missing ID is
404 in every mode — existing behavior; the session is loaded before
deletion.) In `none` mode
the surface is unscoped and `?created_by=` remains a client-side filter.
Admin management lives under `/api/admin/chats` (see Authentication
section) — metadata and lifecycle only.

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

Model validation depends on the configured model source (see
`GET /api/chats/models`):

- **OpenRouter** (`source: "openrouter"`): `model` must be in CM's
  vendor-screened OpenRouter catalog (the same list `GET /api/chats/models`
  serves); unknown slugs return `400` (`INVALID_MODEL`). Validation fails open
  when the catalog has not been fetched (cold start, OpenRouter outage). Omit
  to use `backends.chat.default_model`. Forwarded as `CM_MODEL`.
- **OpenAI-compatible endpoint** (`source: "endpoint"`): `model` must be in the
  endpoint's served model list; unknown slugs return `400` (`INVALID_MODEL`). A
  failed upstream list fetch fails open. Omit to use
  `backends.chat.default_model`. Forwarded as `CM_MODEL`.

Response (`201 Created`): the new `ChatSession` row.

### GET /api/chats/models

Tells the New Chat dialog which model picker to render. The `source` field
selects the mode:

- `"openrouter"` — the chat backend serves chat over OpenRouter. `models` is
  CM's vendor-screened OpenRouter catalog (`id`/`label` = the OpenRouter slug,
  `max_tokens` = the model's context window), served from the server-side
  catalog cache; empty only when the catalog has not been fetched. `default` is
  `backends.chat.default_model`.
- `"endpoint"` — the LLM endpoint is an OpenAI-compatible one (`llm_endpoint.type:
  openai`). `models` is the endpoint's served model list (cached server-side);
  `default` is `backends.chat.default_model`. Also returned, with an empty list,
  when no chat backend is configured — the picker renders nothing.

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

When no chat backend is configured the response is
`{"source": "endpoint", "models": [], "default": ""}`.

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
session is `active` or `warm-idle`, the worker container is ended first.

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

Clear the worker's working memory in place without ending the session. The
server sends `"/clear"` to the chat backend's worker (which re-orients its next
epoch from its own embedded primer), marks every prior transcript row with
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
| 409    | `WORKER_NOT_RUNNING` | Session is not active or warm-idle (no live worker)  |
| 502    | `BACKEND_UNAVAILABLE`| Backend `/clear` send failed (`detail: clear_failed`)|
| 500    | `INTERNAL_ERROR`     | Persistence failure (rare; transcript-side)          |

On a `502` the transcript is left untouched — the operator can retry once
the backend is reachable again. On `500` the worker has already been
cleared but the transcript mark/divider step failed; the session is still
usable, the divider just won't appear in the UI until the next clear.

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
on the per-session SSE hub, then forwards the message to the chat backend.

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
  | `cold`      | Session is idle; no worker container. Starting state and the state after `EndSession`.               |
  | `active`    | Worker container is running and a browser subscriber is present.                                     |
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
