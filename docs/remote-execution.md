# Remote Execution

Remote execution lets a human trigger a worker task from the ContextMatrix web
UI. Two backends carry that work over the same signed-webhook protocol
(`contextmatrix-protocol`, currently v0.11.0):

- **contextmatrix-agent** — the **task backend**. It executes cards: each run
  spawns a disposable Docker container running a multi-model Go orchestrator
  harness that claims the card over MCP, works it in the project repo, and
  reports progress back to the board.
- **contextmatrix-chat** — the **chat backend**. It serves the global chat
  panel: each session runs a long-lived worker container that answers over
  MCP.

ContextMatrix is the coordination layer. It stores cards, manages state, mints
credentials, and drives the backends over webhooks. **It never clones, builds,
or touches project code repositories** — the worker container does, using the
repo URL and credentials CM hands it.

The agent and chat backends live in their own repositories and carry their own
`serve.yaml.example`. This document is the CM-side contract: the webhook
surface, the security model, and the CM configuration that wires a backend in.
For a backend's own runtime knobs (container image, resource limits, ports),
read that backend's `serve.yaml.example`.

## Architecture Overview

```text
  ┌──────────────┐         ┌──────────────────────┐   HMAC       ┌────────────────────────┐
  │  Web UI      │────────►│  contextmatrix       │─── webhook ─►│ contextmatrix-agent    │
  │  (Run btn)   │  REST   │  POST /api/.../run   │   /trigger   │  (task backend)        │
  │  (Console)   │         │  POST /api/.../stop  │   /kill      │  Docker containers     │
  │  (Chat input)│         │  POST /api/.../msg   │   /message   │  running the           │
  │  (Promote)   │         │  POST /api/.../prom  │   /promote   │  orchestrator harness  │
  │              │         │  POST /api/.../s-all │   /stop-all  │                        │
  │              │         │  end-session sub +   │   /end-      │                        │
  │              │         │  reconcile sweep     │   session    │                        │
  │              │         │                      │ /containers  │                        │
  │              │         │                      │◄─────────────┤                        │
  │              │◄────────┤  GET /api/worker/    │   SSE        │                        │
  │              │         │    logs (SSE proxy)  │◄──────────── │  GET /logs             │
  │              │         │  POST /api/agent/    │              │                        │
  │              │         │    status            │◄─── HMAC ────┤  status callback       │
  │              │         │  GET  /api/v1/cards/ │              │                        │
  │              │         │    .../autonomous    │◄─── HMAC ────┤  verify-autonomous     │
  │              │         │                      │              │                        │
  │              │         │  POST /mcp           │◄── Bearer ───┤  MCP tools             │
  └──────────────┘         └──────────────────────┘              └────────────────────────┘
```

The chat backend uses the same protocol on its own callback path (`/api/chat`)
with a chat-specific webhook surface (`/chat/start`, `/chat/end`, `/message`).

**Message paths:**

- **Run Auto / Run HITL / Stop / Stop All** — trigger / kill / stop-all
  webhooks from CM to the task backend.
- **Live log streaming** — the backend exposes a `GET /logs` SSE endpoint; CM
  subscribes to it, buffers, and re-serves the browser as `GET
  /api/worker/logs`. The web UI opens an `EventSource` only while the log
  console panel is open.
- **Chat input** (HITL and chat modes) — the web UI POSTs a message to CM; CM
  generates a `message_id` and forwards `{card_id, project, message_id,
  content}` (card HITL) or `{session_id, message_id, content}` (chat) to the
  backend's `/message` endpoint, then returns the id to the browser.
- **Promote to autonomous** — the web UI POSTs to CM; CM flips the card's
  `autonomous` flag server-side (git commit + SSE event), ensures
  `feature_branch` and `create_pr`, then forwards `/promote` to the task
  backend. The backend verifies the flag via `GET
  /api/v1/cards/{project}/{id}/autonomous` (fail closed) before it acts.

**CM-side interface seams:** `api.TaskBackend` (in `internal/api/backend.go`)
covers the card lifecycle — trigger, kill, stop-all, message, promote,
end-session, health, containers. `internal/backend.Client` is its sole
implementation. The chat lifecycle (`/chat/start`, `/chat/end`, `/message`,
plus the `/logs` SSE bridge) is driven by `internal/chat.backendClient`. Card
progress and usage reporting do **not** flow through these seams — the
in-container worker reports directly through CM's MCP tools (`complete_task`,
`report_usage`, `add_log`).

## Webhook Protocol

### Authentication: HMAC-SHA256 Signing

Every webhook is signed with a shared secret held in both CM
(`backends.<name>.api_key`) and the backend (its own `api_key`). The secret
never travels over the wire. The signature binds the HTTP method, request path,
timestamp, and body, so a valid signature for one endpoint cannot be replayed
against a different endpoint with an identical body (for example `/kill` and
`/end-session`, which both carry `{card_id, project}`). The scheme covers every
signed request in both directions: POST webhooks, the signed GETs (`/logs`,
`/containers`, `/images`, `/health`, `/autonomous`), and the backend's status
callbacks to CM. The canonical implementation is `contextmatrix-protocol`'s
`hmac.go`.

**Signed content:**

```
<METHOD>\n<URI>\n<TIMESTAMP>.<BODY>
```

- `METHOD` — uppercase HTTP method (`POST`, `GET`).
- `URI` — request-target form: path, plus `?<raw-query>` when a query is
  present (`/kill`, `/logs?project=alpha`, `/api/agent/status`). This is the
  value `r.URL.RequestURI()` returns on the receiver. Binding the query string
  keeps two concurrent requests to the same path (`GET /logs?project=A` vs `GET
  /logs?project=B`) from sharing a signature in the same Unix second. Sender and
  receiver must agree: a proxy that rewrites the path or query breaks auth.
- `TIMESTAMP` — Unix seconds, decimal string.
- `BODY` — JSON payload bytes, empty for GET.

**Headers:** `X-Signature-256: sha256=<hex>` and `X-Webhook-Timestamp: <ts>`.

**Verification:** the receiver recomputes the HMAC from method + URI +
timestamp + body and compares in constant time. It rejects timestamps outside
an asymmetric skew window — up to **5 minutes** in the past
(`DefaultMaxClockSkew`), **30 seconds** in the future (`DefaultMaxFutureSkew`) —
so a captured signature cannot be pre-issued or replayed long after the fact. A
per-backend replay cache rejects a `(timestamp, signature)` pair it has already
seen. On the CM side each callback path closes over its own key and replay
cache (`internal/backend/signature_cache.go`), resolved at route-mount time, so
the agent and chat backends verify independently.

### CM → task backend webhooks

All are `POST` with `Content-Type: application/json`, sent to the task
backend's base URL (`backends.agent.url`). The task backend exposes:

| Method + path  | Purpose                                                  |
| -------------- | -------------------------------------------------------- |
| `POST /trigger`      | Start a card run.                                   |
| `POST /kill`         | Stop the container for one card. Idempotent.       |
| `POST /stop-all`     | Stop every container for a project.                |
| `POST /message`      | Deliver a human message to a running HITL card.    |
| `POST /promote`      | Switch a running HITL card to autonomous mode.     |
| `POST /end-session`  | Close a HITL container's stdin so it exits.        |
| `GET /containers`    | List every worker container the backend manages.   |
| `GET /images`        | List node-local worker images, filtered per-tag by `image_list_filters`. |
| `GET /logs`          | Subscribe to the live log SSE stream.              |
| `GET /health`        | Capacity snapshot (running containers, max cap).   |
| `GET /readyz`        | Backend readiness probe (drain-aware).             |

#### POST {agent_url}/trigger

Sent when a human clicks "Run Auto" or "Run HITL" on a `todo` card. CM builds
the payload in `internal/api/backend_run.go`:

```json
{
  "card_id": "ALPHA-042",
  "project": "alpha",
  "repo_url": "https://github.com/example-org/alpha-service.git",
  "mcp_api_key": "optional-bearer-token",
  "base_branch": "develop",
  "worker_image": "ghcr.io/example-org/cm-worker:2026-07-01",
  "interactive": false,
  "model": "deepseek/deepseek-v4-flash",
  "best_of_n": 3,
  "mob": {
    "participants": 3,
    "phases": ["plan", "review"],
    "rounds": 2,
    "budget_factor": 0.75,
    "execute_checkpoints": true,
    "checkpoint_min_tier": "simple",
    "checkpoint_rounds": 3,
    "guests": [{ "name": "laptop", "url": "http://192.168.1.50:8484", "token": "bearer-secret" }]
  },
  "task_skills": ["go-development", "documentation"],
  "selection": { "candidates": [], "favorites": [], "blacklist": [] },
  "verify": { "command": "make test", "timeout_seconds": 600, "env": ["JAVA_HOME"] },
  "git_token": "ghs_...",
  "git_token_expires_at": "2026-07-05T13:00:00Z",
  "llm_endpoint": { "type": "openrouter", "base_url": "", "api_key": "sk-..." }
}
```

`model` is the backend's configured `backends.agent.default_model`; it is empty
when that field is unset, and the agent then resolves its own default. Per-card
model pins are applied agent-side, not here.

`worker_image` is the project's `remote_execution.worker_image` when set,
otherwise empty and the backend falls back to its configured base image. This
is the task backend's **language-toolchain seam** — it answers "what toolchain
does this project's code need" for card runs. See
[Worker image split](#worker-image-split) for how this differs from the chat
backend's own `chat_worker_image`. The default worker image already carries
Go, Node, Python, and Rust toolchains; a project in another ecosystem sets
`worker_image` to a custom image built on the worker base.

`best_of_n` is present only when the card's stored value is `>= 2`. CM clamps
it down to the configured `best_of_n.max_candidates` before sending, since a
card can carry a value that exceeds the current max if the operator lowered it
after the card was set. See [Best-of-N run](#best-of-n-run) for what the agent
does with it.

`mob` is present only when the card's `mob_participants` is `>= 2`.
`participants` is the card value clamped to the current
`mob.max_participants`; `rounds` and `budget_factor` carry
`mob.default_rounds` and `mob.budget_factor`; `execute_checkpoints`,
`checkpoint_min_tier`, and `checkpoint_rounds` mirror the server flags.
`guests` is the card's `mob_guests` resolved through the registry into full
`{name, url, token}` specs — unknown names are dropped with a `mob-warning`
activity entry. The `execute` phase is dropped (with a warning) when
`mob.execute_checkpoints_enabled` is off; when the flag is on and the same
payload carries `best_of_n >= 2`, mob coding takes trigger-time priority
instead — `best_of_n` is zeroed with a warning rather than the phase being
dropped. Guest tokens are bearer secrets: the backend stages them into the
per-run secrets file, never plain container env, and registers them with
its log redactor. See [Mob sessions](#mob-sessions).

`selection` (`protocol.SelectionContext`) carries the auto-selection inputs:
the rated model `candidates` from CM's cached catalog, the operator `favorites`
merged global-over-project, the self-learning `blacklist` of models CM has
marked incapable, an `outcome_floor`, and per-model outcome stats. The agent
picks the orchestrator, coder, and reviewer models from these inputs. It is
omitted when CM has no model catalog configured.

`verify` is the resolved verify gate. CM merges the card's `verify` over the
project's field by field and omits it when nothing resolves; the agent then
falls back to its own detection. When present, the agent runs `command` via
`bash -c` bounded by `timeout_seconds`, passing the named `env` variables
through by name (never value) on top of its scrubbed allowlist.

`git_token` is a short-lived credential for the project repo, minted by CM from
the project's `github_credential` binding (or the instance `github.*`
credential when unbound). A broken binding rejects the run with `409` before
any webhook is sent — CM never substitutes the instance credential for a named
binding. `git_token_expires_at` is RFC3339, absent for PAT-backed credentials
(absent means "no refresh needed"). App-backed tokens live ~1h; the backend
re-mints mid-run via `GET /api/agent/git-credentials` (see
[GitHub token refresh](#github-token-refresh)).

`llm_endpoint` carries CM's `llm_endpoint` config (type, base URL, key) so the
inference key is administered in one place. It is omitted when CM has no
endpoint configured; the backend then uses its own local configuration.

`task_skills` is the resolved skill list — see [Task skills](#task-skills).

`base_branch` is omitted when unset. When present, the backend clones against
it and opens PRs against that branch instead of the repository default.

`feature_branch` and `create_pr` are auto-enabled on the card for **all** run
triggers, autonomous and HITL, so a feature branch and PR are always created
regardless of the launch mode.

#### Task skills

CM resolves the skill list `card.skills > project.default_skills > nil` and
ships it in the `task_skills` field: a `nil` list is omitted (the backend
mounts its full set), an empty array means "explicit none — mount nothing", and
a populated array is the exact subset.

The backend does not receive the skill files over the webhook. Instead it
fetches a `{git_remote_url, ref}` pointer from CM's `GET
/api/agent/task-skills-source` (chat: `GET /api/chat/task-skills-source`) and
clones the task-skills repo itself, so CM stays the single source of truth for
which skills exist. CM derives that pointer from its own `task_skills.dir` and
optional `task_skills.git_remote_url`. The response also carries a best-effort
instance-scoped git token so the backend can clone a private skills repo. The
backend then exposes the resolved subset to its in-container worker via the
`Skill` tool.

On startup, when `task_skills.dir` already contains a `.git` directory **and**
`task_skills.git_remote_url` is non-empty, CM runs `git pull --ff-only`
(60-second timeout) before opening any listener. The log line `task-skills
startup pull: ok` confirms success; a failure logs `task-skills startup pull
failed` as a warning and does not block startup.

#### POST {agent_url}/kill

Sent when a human clicks "Stop", or by the end-session subscriber / reconcile
sweep when a card reaches a terminal state.

```json
{ "card_id": "ALPHA-042", "project": "alpha" }
```

**Idempotent.** The backend returns `200` whether it tracked the container,
reached past a divergent tracker to force-remove a labeled Docker container, or
found nothing to do. Returning `200` in every case means CM's retry logic never
has to distinguish "not found" from "killed".

#### GET {agent_url}/containers

Returns every worker container the backend manages, running or exited. Signed
GET (empty body). Consumed by CM's reconcile sweep as the authoritative answer
to "what is actually running right now" — independent of the backend's
in-memory tracker and of CM's card-level `worker_status`.

```json
{
  "ok": true,
  "containers": [
    {
      "container_id": "778fe6561d75abc...",
      "container_name": "cm-agent-alpha-ALPHA-042",
      "card_id": "ALPHA-042",
      "project": "alpha",
      "state": "running",
      "started_at": "2026-07-01T10:30:00Z",
      "tracked": false
    }
  ]
}
```

`tracked` reflects the backend's tracker state at response time. `tracked:
false` with `state: "running"` is the divergence signature the sweep is built
to catch — a container Docker is running that the tracker has forgotten.

#### GET {agent_url}/images

Returns the worker images present on the backend's node — the source for CM's
`GET /api/backends/agent/images` proxy (see [Backend capacity: GET
/api/backend/health](#backend-capacity-get-apibackendhealth) for the sibling
health probe, and `docs/api-reference.md` for the CM-side route). Signed GET
(empty body).

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

The backend keeps only tags containing one of its configured
`image_list_filters` substrings (default `[contextmatrix-agent]`; the chat
backend's default is `[contextmatrix-chat]`), and skips dangling images (no
repo tags). A Docker daemon failure returns a generic `502 upstream_failure` —
see [Backend response format](#backend-response-format).

#### POST {agent_url}/stop-all

```json
{ "project": "alpha" }
```

Stops every container for the project. The backend returns a
`StopAllResponse` (`{ok, total, stopped, failed, results[]}`); a per-card
failure flips `ok` to `false` and the status to `207`.

#### POST {agent_url}/message

Sent when a human submits a message while a container runs in HITL mode. Card
HITL payload (CM generates `message_id`):

```json
{
  "card_id": "ALPHA-042",
  "project": "alpha",
  "message_id": "5a7f3c1e-9d2b-4e1a-bd8c-6f9e2a3c4d5f",
  "content": "Please focus on the authentication module first."
}
```

Chat payload (CM's chat manager sends this when a user submits a turn):

```json
{
  "session_id": "01K8ZQH7R3VYJE9XPK4MBWN5T2",
  "message_id": "5a7f3c1e-9d2b-4e1a-bd8c-6f9e2a3c4d5f",
  "content": "Show me the diff between v1 and v2."
}
```

Exactly one of `(card_id + project)` or `session_id` is set —
`MessagePayload.IsChat()` dispatches on it. The backend writes the content to
the container's stdin as a stream-json `user` message and echoes it as a `user`
log entry so the browser sees it in the console. Content is capped at 8 KiB.

#### POST {agent_url}/promote

Sent when a human clicks "Switch to Autonomous" on a running HITL card.

```json
{ "card_id": "ALPHA-042", "project": "alpha" }
```

The backend runs a strict, fail-closed sequence:

1. **Verify the flag.** It calls `GET
   {contextmatrix_url}/api/v1/cards/{project}/{id}/autonomous` (HMAC-signed,
   empty body) and checks that the body `{"autonomous": bool}` is `true`. CM
   already flipped the flag before sending this webhook, so the GET is a
   read-only confirmation. On any failure — network error, non-2xx, or
   `autonomous != true` — the backend returns `502` and does **not** touch
   stdin; the card stays in HITL mode.
2. **Inject the canned message.** It emits a `system` log entry, then writes a
   stream-json user message to stdin telling the worker to re-read the card at
   its next gate and proceed on the autonomous branch without waiting for
   input.
3. **Close stdin.** The worker sees EOF, finishes in-flight work, and exits
   cleanly rather than idling to the container timeout.

#### POST {agent_url}/end-session

Sent by CM when a card tied to a HITL container reaches a terminal state
(`done` or `not_planned`) and is released. Closes the container's stdin so the
worker sees EOF and exits.

```json
{ "card_id": "ALPHA-042", "project": "alpha" }
```

An event-bus subscriber in CM (`internal/backend/endsession.go`) fires this
when it sees a card with `state ∈ {done, not_planned}` and `assigned_agent ==
""`. `worker_status` is deliberately **not** part of that predicate — see
[Terminal-state cleanup](#terminal-state-cleanup) for why, and why
`/end-session` is always followed by an unconditional idempotent `/kill`.

### Worker image split

`remote_execution.worker_image` and `remote_execution.chat_worker_image` are a
clean cut, not a shared field:

- `worker_image` feeds `/trigger` only (card runs on the task backend).
- `chat_worker_image` feeds `/chat/start` only (chat sessions on the chat
  backend).
- Either one empty means "use that backend's own configured `base_image`" —
  there is no fallback to the other field. The two image families bake
  different entrypoints, so a task image on a chat session (or vice versa)
  would not run correctly.
- `chat_worker_image` applies to every chat session — chat is gated only by
  the chat backend's own configuration, never by the task backend.
- Both fields share the same hygiene validation (charset-restricted, capped at
  512 bytes, whitespace-trimmed) and the same pointer-merge PUT semantics on
  `PUT /api/projects/{project}`: an omitted field preserves the stored value,
  an explicit empty string clears it. See `docs/data-model.md` § Project
  configuration.

### CM → chat backend webhooks

The chat backend exposes `POST /chat/start`, `POST /chat/end`, `POST /message`,
and the signed GETs `/logs` / `/images` / `/health` / `/readyz`. Chat
containers run their own worker image (the chat backend's `base_image`, or the
project's `remote_execution.chat_worker_image` override — never the task
backend's `worker_image`) but stay long-lived (no per-task cleanup) and
dispatch on the session ID. `GET /images` behaves exactly as it does for the
task backend, filtered by the chat backend's own `image_list_filters` (default
`[contextmatrix-chat]`).

#### POST {chat_url}/chat/start

```json
{
  "session_id": "01K8ZQH7R3VYJE9XPK4MBWN5T2",
  "project": "contextmatrix",
  "repo_url": "https://github.com/mhersson/contextmatrix.git",
  "worker_image": "ghcr.io/example-org/contextmatrix-chat-worker:2026-07-01",
  "mcp_api_key": "<forwarded as CM_MCP_API_KEY>",
  "model": "anthropic/claude-sonnet-4",
  "resume": { "turns": [], "clipped": false, "original_seq": 0 },
  "llm_endpoint": { "type": "openrouter", "base_url": "", "api_key": "sk-..." },
  "git_credentials_token": "01K8ZQH7....<base64url mac>"
}
```

`project` and `repo_url` are both optional — omit for a cross-project chat.
`mcp_api_key` may be empty when CM's MCP listener has no auth (loopback dev
mode). `model` is required: the chat backend has no server-side default, so CM
always populates it from the session row, falling back to
`backends.chat.default_model`. The wire field is still named `worker_image`
(the chat protocol's own field), but CM populates it from the project's
`remote_execution.chat_worker_image` on every cold open — never from
`remote_execution.worker_image`. See [Worker image split](#worker-image-split).

`resume` is the rehydration payload built by CM's transcript builder. When
present, the backend materializes it inside the container and switches the
worker into its rehydration prompt so it restores context before the first
user turn. When absent, the worker starts fresh.

`git_credentials_token` is the chat credential story, deliberately different
from `/trigger`'s upfront `git_token`: a chat session is long-lived and can be
cross-project, so there is no single card-scoped repo to mint a token for ahead
of time. CM hands the worker a deterministic per-session bearer —
`<session_id>.<base64url HMAC-SHA256 mac>`, keyed by the chat backend's
`api_key` — and the worker fetches a fresh per-repo credential on demand from
`GET /api/worker/git-credentials`. CM never persists the bearer; it re-derives
and compares. The field is empty when no chat-backend `api_key` is configured;
the chat backend then fails the session closed.

The success body is `202 {"ok": true, "container_id": "..."}`.

#### POST {chat_url}/chat/end

```json
{ "session_id": "01K8ZQH7R3VYJE9XPK4MBWN5T2" }
```

Closes the container's stdin and stops it. A second call returns `404` because
the tracker entry is gone.

### Task/chat backend → CM: log stream

#### GET {backend_url}/logs

Streams live log frames via Server-Sent Events. CM subscribes to this — the
browser never calls it directly. Signed GET (empty body).

| Query param  | Effect                                                    |
| ------------ | -------------------------------------------------------- |
| `project`    | Filter to a single project. Omit to receive all.         |
| `session_id` | Filter to one chat session (the chat log bridge).        |

Each event is a JSON `protocol.LogEntry`:

```json
{
  "ts": "2026-07-01T12:34:56.789Z",
  "card_id": "ALPHA-042",
  "project": "alpha",
  "type": "text",
  "content": "[round 1] seat-1 (correctness): the parser change misses...",
  "agent": "seat-1",
  "model": "z-ai/glm-5.2"
}
```

Chat frames set `session_id` instead of `card_id`. `type` is one of:

| type        | Meaning                                                               |
| ----------- | --------------------------------------------------------------------- |
| `text`      | Assistant text block.                                                 |
| `thinking`  | Assistant thinking block.                                             |
| `tool_call` | A tool call summary.                                                  |
| `stderr`    | A raw stderr line from the container.                                 |
| `system`    | Container lifecycle event (started, completed, failed, canceled).     |
| `user`      | A human message echoed by the backend's `/message` handler.           |
| `usage`     | Per-turn token accounting. `content` is empty; `usage` carries `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens`, and `model` carries the responding model ID. |

`agent` is the optional speaker attribution for mob session discussion frames:
the agent backend's log bridge maps its `discussion` JSONL events to
`type: "text"` frames carrying `agent` (`seat-1`..`seat-N`,
`guest-<name>`, `moderator`, `human`). CM threads the field through its
session-log buffer into the browser SSE stream, where the chat panel
renders it as a speaker chip. Ordinary single-agent frames omit it.

`model` is the optional LLM model slug that produced the speaker's turn
(e.g. `z-ai/glm-5.2`, `anthropic/sonnet-5`). It appears alongside `agent`
on mob session discussion frames and is absent on ordinary single-agent
frames and human participants. The chat panel renders it as a second pill
(in the `--purple` accent) on the same line as the speaker chip. The agent
backend populates it from its seat configuration; if it does not, the field
is simply absent from the log frame.

The backend redacts common credential patterns (GitHub tokens, API keys, Bearer
tokens) before publishing, and sends keepalive comments to hold the connection
open through proxy idle timeouts.

### Task backend → CM: callbacks

Callback POSTs are HMAC-signed with the backend's own `api_key`. Each backend's
callback endpoints mount at its fixed callback path — `/api/agent` for the task
backend, `/api/chat` for the chat backend (`config.AgentCallbackPath` /
`config.ChatCallbackPath`; the agent and chat repos hardcode these). Those paths
are exempt from the CSRF guard because they carry no browser POST. CM rejects a
missing or invalid signature with `403 INVALID_SIGNATURE`.

#### POST /api/agent/status

The backend reports a worker-status transition.

```json
{
  "card_id": "ALPHA-042",
  "project": "alpha",
  "worker_status": "running",
  "message": "container started"
}
```

CM validates the value with `board.ValidateWorkerCallbackStatus` (the backend
may set `running`, `failed`, or `completed`). CM applies a post-terminal
normalization: a `failed` or `killed` callback that arrives after the card has
already reached `done` / `not_planned` is rewritten to `completed` (activity
message "container cleaned up after run completed") so a cleanup-driven kill
does not retroactively flip a successful run to failed. Task completion is
**not** reported here — the in-container worker uses the MCP `complete_task`
tool.

#### GET /api/v1/cards/{project}/{id}/autonomous

Read-only, called by the task backend during `/promote` to fail-closed confirm
the card's autonomous flag before writing to stdin. Signed GET.

```json
{ "autonomous": true }
```

Registered only when a task backend is configured. Missing or invalid HMAC
returns `403`; an unknown card returns `404`.

#### GET /api/agent/git-credentials

Re-mints the project-scoped git token for a running card so a long run can
refresh past the ~1h GitHub App installation-token TTL. Signed GET; the card
must exist and be actively running. Fail-closed on the project binding: a broken
provider never falls back to the instance credential.

#### GET /api/agent/task-skills-source

Serves the `{git_remote_url, ref}` skills pointer plus a best-effort
instance-scoped git token — see [Task skills](#task-skills). The chat backend
uses the identical `GET /api/chat/task-skills-source`.

### CM operator endpoints

These are the CM-side handlers the web UI calls. They wrap the outbound task
webhooks, enforce card-state and per-project checks, and update CM bookkeeping
(`worker_status`, `feature_branch`, `create_pr`, `autonomous`). All five gate on
`isNonHumanAgent` — an agent hitting them with a non-`human:*` `X-Agent-ID`
gets `403 HUMAN_ONLY_FIELD`; an absent header counts as human (UI = human, per
the CLAUDE.md trust model).

| Endpoint                                          | Behavior                                                                                                                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /api/projects/{project}/cards/{id}/run`     | Body `{"interactive": bool}` (empty body OK). Requires state `todo` and `worker_status ∉ {queued, running}`. Auto-patches `feature_branch=true, create_pr=true`, sets `worker_status: queued`, sends `/trigger`. Returns `202`. |
| `POST /api/projects/{project}/cards/{id}/stop`    | Requires `worker_status ∈ {queued, running}`. Sends `/kill`, then sets `worker_status: killed`. Returns `202`.                                                             |
| `POST /api/projects/{project}/cards/{id}/message` | Body `{"content": "..."}` (≤ 8 KiB). Requires `worker_status == running`. CM generates the `message_id`, forwards to `/message`, returns `202 {"ok": true, "message_id": "..."}`. |
| `POST /api/projects/{project}/cards/{id}/promote` | Idempotent. When `autonomous` is already true, short-circuits with `202` (no outbound webhook, preventing verify recursion). Otherwise flips `autonomous`, ensures `feature_branch/create_pr`, sends `/promote`, and rolls all three back if the webhook fails. Returns `202`. |
| `POST /api/projects/{project}/stop-all`           | Sends `/stop-all`, then flips `worker_status` to `killed` for every project card in `{queued, running}`. Returns `200 {"affected_cards": [...]}` — or `207` with `failed_to_update` when the webhook succeeded but a CM-side status write drifted. |

**CM operator error codes** (status / `code`):

| Code                 | Status    | Meaning                                                                     |
| -------------------- | --------- | --------------------------------------------------------------------------- |
| `BACKEND_DISABLED`   | 503       | No execution backend is configured.                                          |
| `WORKER_CONFLICT`    | 409       | Card is already `queued` or `running` on a worker.                          |
| `WORKER_NOT_RUNNING` | 409       | Card is not currently being executed (stop / message / promote).            |
| `BACKEND_UNAVAILABLE`| 502       | The outbound webhook to the backend failed.                                 |
| `INVALID_TRANSITION` | 409       | Run refused because the card is not in `todo`.                              |
| `HUMAN_ONLY_FIELD`   | 403       | `X-Agent-ID` was a non-human agent.                                         |
| `CONTENT_TOO_LARGE`  | 413       | Message body exceeds 8192 bytes.                                            |
| `VALIDATION_ERROR`   | 422 / 409 | Empty message `content` (422), or an unresolvable project credential (409). |
| `BAD_REQUEST`        | 400       | Malformed JSON body.                                                        |

### Backend response format

A 2xx webhook returns `protocol.SuccessResponse` (`{ok: true, message?,
message_id?}`); a non-2xx returns `protocol.ErrorResponse` (`{ok: false, code,
message}`). `code` is a stable enum from `protocol/codes.go` — branch on it,
not on `message`:

| Code               | Status | Meaning                                                             |
| ------------------ | ------ | ------------------------------------------------------------------ |
| `invalid_json`     | 400    | Body could not be decoded as JSON.                                 |
| `invalid_field`    | 400    | A required field is missing or invalid (message names the field).  |
| `unauthorized`     | 401    | HMAC signature missing, invalid, or timestamp out of window.       |
| `not_found`        | 404    | No container tracked for `(project, card_id)` / `session_id`.      |
| `conflict`         | 409    | State conflict (card already tracked, or wrong container mode).    |
| `limit_reached`    | 429    | `max_concurrent` reached.                                          |
| `too_large`        | 413    | A field exceeds its size cap (the `/message` content cap).         |
| `upstream_failure` | 502    | An upstream dependency failed (CM's verify-autonomous check, or the local Docker daemon for `/images`). |
| `draining`         | 503    | Graceful shutdown started; mutating endpoints refuse new work.     |
| `internal`         | 500    | Server-side bug; the message is fixed and never echoes `err`.      |

### Retry policy

CM retries failed webhooks with exponential backoff (`internal/backend`):

- 3 attempts total, waits of 1s, 2s, 4s (`BackoffBase`).
- Retries only on network errors and HTTP 5xx.
- HTTP 4xx fails immediately.
- Per-request timeout: 10 seconds.

## Worker lifecycle

A `/trigger` starts one disposable container that runs the card end to end:
clone the repo, claim the card over MCP, plan, execute, document, review,
integrate, and complete via `complete_task`. The container exits when the run
finishes, and the backend removes it. A `/kill` destroys the container
immediately — uncommitted work is discarded.

CM is the single authority on whether a container should be running. Two
mechanisms enforce that, reasoning from different truths so a bug in either
cannot silently hide a live container.

### Terminal-state cleanup

A HITL container's worker process does not exit when its stdin closes — in
stream-json mode it treats EOF as "no more input for now" and keeps running. A
card that reaches a terminal state (`done` / `not_planned`) and is released must
therefore be stopped by CM explicitly.

1. **Event subscriber (fast path).** `internal/backend/endsession.go` watches
   the event bus for `card.released` and `card.state_changed`. When it sees
   `state ∈ {done, not_planned}` and `assigned_agent == ""`, it sends
   `/end-session` (a polite stdin close) followed by an unconditional `/kill`.
   `/kill` is idempotent, so firing against an already-dead container costs one
   `200` no-op. `worker_status` is deliberately not consulted: a backend's
   completion callback flips the field before Docker cleanup actually succeeds,
   so gating on it would let a drifted status turn into a permanent leak.
2. **Reconcile sweep (Docker-authoritative backstop).**
   `internal/backend/reconcile.go` runs every `backends.agent.reconcile_interval`
   (default **60s**). Each tick it calls `GET /containers` and, for every
   container Docker is actually running, kills it when the card is missing, is
   in `done` / `not_planned`, or has outlived `ContainerMaxAge`. The sweep
   reasons only from Docker ("is it running?") and the card store ("should it
   be?") — never from `worker_status` — which keeps the drift bug unreachable.
   **This sweep is the agent backend's only reconcile mechanism; the backend has
   no internal reconcile loop of its own.** Setting `reconcile_interval` to
   `"0s"` disables the sweep and is not recommended: the event path is
   best-effort (events drop on subscriber overflow and are never delivered
   during a CM restart), so without the sweep a single dropped event leaks a
   container until the backend's own container timeout.

### Best-of-N run

Best-of-N is task-backend only: CM sends `best_of_n` only to the agent, and
only when the card's value is `>= 2`. A Best-of-N run still gets exactly **one**
worker container. Inside it, after the plan phase, the agent adds N git
worktrees on local-only branches cut from the plan-approved base, assigns each
candidate a distinct auto-selected coder model and its own budget ledger, and
races them concurrently. Candidates never push and make no per-candidate board
writes, but the run claims each subtask once when the first candidate reaches it
(the board shows it `in_progress`, the parent auto-transitions), held alive by a
single heartbeater until the winner's completions replay after judging. A judge
phase (reviewer-role selection, complex tier) picks a winner, and the
orchestrator adopts it onto the main clone with `git reset --hard <winner
HEAD>` before the run's first push; losing worktrees are removed. Wall-clock
time is roughly the slowest candidate plus the judge pass, and the per-card
budget ceiling scales to `MaxCardCost × (N + 1)`.

### Mob sessions

Mob session is task-backend only: CM sends `mob` only to the agent, and only
when the card's `mob_participants` is `>= 2`. A mob session run still gets
exactly **one** worker container. Inside it, the orchestrator hosts every
internal seat behind a loopback a2a-go JSON-RPC server (127.0.0.1, port never
published, bearer-protected) and acts as the only A2A client — dialing
loopback seats and registered guest URLs over the same wire. Plan and review
phase bodies convene a discussion (blind round 0, then critique rounds up to
the payload's `rounds`), and the decision model synthesizes the group's
answer into the phase's existing output format. Discussions degrade, never
fail: quorum below 2 responding seats, engine errors, or an exhausted mob
session budget (`budget_factor × max card cost`) fall back to the existing
solo path.

Wire conventions (shared by internal seats and guest shims): message bodies
are markdown transcript deltas with entries formatted
`[round N] author (lens): text`; control metadata rides A2A
`Message.metadata` under key `cm_mob` as
`{"control": "round" | "close", "round": <n>}`, with missing/unknown
metadata treated as `"round"`. Live transcript lines reach the board as
`discussion` JSONL events that serve's log bridge maps to `text` log frames
with the `agent` attribution field (see the log-stream section above); seat
sub-run internals are emitted under debug kinds the bridge deliberately does
not map.

Composition with Best-of-N: plan and review mob discussions compose freely
with a Best-of-N execute race. The `execute` phase does not: when a card
requests both, **mob coding wins** — the trigger zeroes `best_of_n` with a
`mob-warning` activity entry. When `mob.execute_checkpoints_enabled` is
`false`, the `execute` phase is dropped at trigger time instead and
Best-of-N runs normally.

Execute checkpoints: with `execute` in the card's phases (and the server
flag on, its default), the worker convenes a non-blind discussion after
each committed subtask at or above `mob.checkpoint_min_tier` (default
`simple`): the seats argue over the subtask's diff (40 KB cap, diffstat
fallback) for `mob.checkpoint_rounds` rounds (default 3), and the
moderator returns `proceed` or `revise` with at most 3 fixes. A revise
triggers one fix pass on the same coder before the push; the revised diff
is not re-checkpointed. Checkpoints are best-effort: any failure logs and
the run proceeds. Transcripts stream as `discussion` events; the card gets
one activity entry per checkpoint outcome (proceed, revise, or unparsable),
plus a second entry (`revise skipped — budget exhausted`) when a revise
verdict then hits the card budget ceiling. The checkpoint *discussion* draws
from the shared mob budget term (`mob.budget_factor × max card cost`) —
operators enabling the `execute` phase on multi-subtask cards should
consider raising `mob.budget_factor` so plan and review discussions are not
starved. The revise fix pass itself spends from the card budget like any
other coder run, and is skipped once that budget is exhausted.

### GitHub token refresh

The task backend receives a short-lived `git_token` in the `/trigger` payload.
Because a GitHub App installation token lives only ~1h and a run can outlast it,
the backend re-mints mid-run from `GET /api/agent/git-credentials` (card-scoped,
fail-closed). PAT-backed credentials carry no expiry and need no refresh.

The chat backend uses a different flow: no upfront token, a per-session bearer,
and per-repo credentials fetched on demand from `GET
/api/worker/git-credentials` — see the `/chat/start` payload above and
`docs/api-reference.md` § Worker & Backend Endpoints for the full contract.

## Security Model

- **Per-backend HMAC keys.** Each backend has its own shared secret
  (`backends.<name>.api_key`, ≥ 32 chars). CM signs outbound webhooks and the
  `/logs` subscription; the backend signs its status callbacks. The agent and
  chat callback spaces verify with independent keys and replay caches, so one
  backend's key never authenticates against the other's endpoints.
- **Per-run MCP bearer.** When `mcp_api_key` is set, CM forwards it in the
  trigger / chat-start payload; the backend injects it into the container's MCP
  configuration, and every MCP request from the worker carries `Authorization:
  Bearer <key>`.
- **CM-provisioned git credentials.** CM mints all git tokens — a card-scoped
  token per run (refreshed on demand), or a per-session bearer that fetches
  per-repo credentials on demand for chat. A broken project credential binding
  fails the run closed; CM never silently substitutes the instance credential.
- **CM-provisioned LLM endpoint.** The inference endpoint and key are
  administered once in CM's `llm_endpoint` and forwarded to the backend per run,
  so the model key is rotated in one place.
- **Human-only controls.** Only humans (no `X-Agent-ID`, or a `human:*` prefix)
  can trigger, stop, message, or promote a run, or set the `autonomous`,
  `feature_branch`, and `create_pr` flags. A worker inside a container cannot
  escalate itself to autonomous mode — promotion is verified server-side.

### Global kill switch

Disable or remove the task backend from `backends` to stop all card execution:
the run button disappears and trigger endpoints return `503 BACKEND_DISABLED`.
This is a restart-required change — backends are read once at startup.

## Interactive Mode

"Run HITL" sends `interactive: true`. CM forces it off for autonomous cards
server-side (defense in depth: a stray trigger cannot push an autonomous card
down the HITL path). In HITL mode the worker starts plan drafting immediately
and pauses at its built-in gates (plan approval, execution decision, review),
waiting on human input delivered through the chat console.

**Message flow:**

```
Web UI (chat input) → CM POST /api/projects/{project}/cards/{id}/message
                     → task backend POST /message
                     → container stdin (stream-json user message)
                     → LogEntry{type: "user"}  (echoed to the browser console)

Web UI (promote btn) → CM POST /api/projects/{project}/cards/{id}/promote
                      → CM flips card autonomous=true (git commit + SSE event)
                      → task backend POST /promote
                      → backend GET /api/v1/cards/{project}/{id}/autonomous  (fail-closed)
                      → LogEntry{type: "system", content: "promoted to autonomous mode"}
                      → container stdin (canned autonomous-mode message)
                      → backend closes stdin (EOF → worker exits after work done)
```

The promote `system` log entry is published before the stdin write so the
browser shows the mode switch in order even if the write stalls. If CM's
outbound `/promote` fails, CM rolls back `autonomous`, `feature_branch`, and
`create_pr` so the card's declared mode matches the worker's actual mode.

## Log Streaming Architecture

The live-log pipeline moves worker output to the browser in four hops:

```
worker stdout  →  backend log bridge  →  protocol.LogEntry over backend GET /logs (SSE)
               →  CM session-log manager (buffer + fan-out)
               →  browser GET /api/worker/logs (SSE)
```

- **Backend log bridge.** The backend demultiplexes each container's
  stdout/stderr, parses it into `protocol.LogEntry` frames, redacts secrets,
  and publishes them on its `GET /logs` SSE stream — filterable by `project`
  (card mode) or `session_id` (chat mode).
- **CM session-log manager** (`internal/backend/sessionlog`). CM opens one
  long-lived HMAC-signed upstream connection per card (or per project, or per
  chat session), buffers frames in a bounded ring (dual cap: 2000 events or
  1 MiB, whichever first), and fans them out to browser subscribers
  snapshot-first-then-live, so a reconnecting tab replays history including any
  pending HITL question. The manager revives an idle-swept session on the next
  connect, retries the upstream with capped backoff, and closes sessions that
  go idle past the TTL.
- **Browser SSE** (`GET /api/worker/logs`). `internal/api/worker_logs.go`
  serves the web UI. It requires a valid `project` query parameter and selects
  the card-scoped path when `card_id` is present, the project-scoped path
  otherwise. Both clear the write deadline (SSE connections outlive the
  server's `WriteTimeout`) and send a keepalive comment every 30 seconds to
  survive proxy idle timeouts. Marker frames tell the client when a session has
  ended (`{"type":"terminal"}`) or the buffer overflowed
  (`{"type":"dropped"}`).
- **Chat variant.** For chat, CM's chat manager consumes the same `/logs`
  stream keyed by `session_id`, persists each frame to the shared `ops.db`, and
  republishes on the per-session chat SSE hub. `usage` frames update the
  session's context-token counter.

### Backend capacity: GET /api/backend/health

The web UI's capacity meter reads `GET /api/backend/health`, which proxies the
task backend's `GET /health` and returns `{ok, running_containers,
max_concurrent}`. `internal/api/backend_health.go` caches the probe for a short
TTL and coalesces concurrent callers through singleflight, so many open tabs and
a backend outage never storm the backend with redundant probes. It returns
`503 BACKEND_DISABLED` when no task backend is configured and `502
BACKEND_UNAVAILABLE` when the backend is unreachable; callers fail soft (hide
the meter).

### Backend worker images: GET /api/backends/{backend}/images

The project-settings image pickers read `GET /api/backends/{backend}/images`
(`backend` is `agent` or `chat`), which proxies that backend's own `GET
/images`. `internal/api/backend_images.go` caches each backend's probe result
for 30 seconds behind a singleflight, coalescing concurrent callers; the probe
itself runs on a detached context so a departing caller's cancellation cannot
poison the cache for the next one. The 30-second window is longer than the
health cache and is load-bearing, not just an optimization: concurrent
same-second signed GETs would otherwise produce identical HMAC signatures and
collide in the backend's replay cache.

The route is session-guarded like the rest of the API in multi mode, and
additionally admin-gated inside the handler — the same gate as the
project-settings `PUT` the picker feeds; open in none mode, like the rest of
the API. It returns `404 BACKEND_NOT_FOUND` for an unknown `{backend}` value,
`503 BACKEND_DISABLED` when that backend isn't configured, and `502
BACKEND_UNAVAILABLE` when the upstream probe fails; callers fail soft (the
picker degrades to "Backend default" plus any saved value). See
`docs/api-reference.md` for the full response shape.

## Card worker status

`worker_status` tracks the container lifecycle independently of the card's
workflow state:

| worker_status | Meaning                                                       |
| ------------- | ------------------------------------------------------------ |
| (empty)       | Not associated with a worker.                                |
| `queued`      | Trigger sent, container not yet started.                     |
| `running`     | Container is running.                                        |
| `failed`      | Container crashed or the trigger webhook failed.             |
| `killed`      | A human stopped the task.                                    |
| `completed`   | Container finished successfully.                             |

The backend sets `running` / `failed` / `completed` through the `POST
/api/agent/status` callback. CM sets `queued` at trigger, `killed` on stop /
stop-all, and `failed` when its own outbound trigger webhook fails. Transitions
to `done` / `not_planned` do not clear `worker_status`; it is informational
bookkeeping for the UI. Reaching a terminal `worker_status` (`failed`,
`killed`, or normalized `completed`) also clears `assigned_agent` and
`last_heartbeat` and flushes pending deferred commits so the boards repo
reflects the final state immediately. CM emits `worker.triggered` /
`worker.started` / `worker.failed` / `worker.killed` events on its internal bus
as the status changes, which drive the UI's SSE updates.

## Configuration Reference

### ContextMatrix (`config.yaml`)

CM drives backends through the typed `backends` map — a closed set of two entry
names, `agent` and `chat`. Their roles and callback paths are fixed and not
selectable. Backends are read once at startup; any change requires a restart.
An unknown backend name fails startup rather than being silently ignored. See
`config.yaml.example` for the fully commented block.

```yaml
# MCP endpoint authentication (optional). Forwarded to the backend so the
# in-container worker can authenticate to CM's MCP endpoint.
mcp_api_key: "your-bearer-token"

backends:
  agent:
    url: "http://localhost:9090"    # base URL of the task backend
    api_key: "shared-hmac-secret"   # ≥ 32 chars; must match the backend's api_key
    enabled: true                   # default true; false = inert placeholder
    default_model: "deepseek/deepseek-v4-flash" # optional; empty ⇒ agent resolves its own default
    reconcile_interval: "60s"       # backstop sweep tick; "0s" disables it
    # Catalog + selection inputs (agent only):
    aa_api_key: ""                  # Artificial Analysis key for rating models
    model_allowlist: []             # restricts the catalog (openrouter type)
    # favorites, aa_model_map, model_priors — see config.yaml.example
  chat:
    url: "http://localhost:9091"
    api_key: "another-hmac-secret"
    enabled: true
    default_model: "anthropic/claude-sonnet-4" # REQUIRED when enabled

# Global chat-panel behavior. Chat data persists in the shared ops.db
# (op_store.db_path); there is no separate chat database. The chat model
# source is the configured LLM endpoint (llm_endpoint), with
# backends.chat.default_model as the cold-open fallback.
chat:
  idle_ttl: 1h
  max_concurrent: 8
  resume_budget_tokens: 40000
  rehydration_timeout: 10m
```

**Validation.** Each enabled entry requires `url` and an `api_key` of at least
32 characters (`MinBackendAPIKeyLength`). A disabled entry (`enabled: false`)
skips further validation. `reconcile_interval` is valid on the `agent` entry
only (default 60s); it — and every other unknown per-entry key — fails startup
on the `chat` entry. `default_model` is required on an enabled `chat` entry.
Decoding is strict: an unknown backend name or a stale per-entry field fails
startup loudly rather than being silently dropped.

**Environment overrides.** `CONTEXTMATRIX_BACKEND_<NAME>_*` maps to each entry
(`<NAME>` = `AGENT` or `CHAT`):

- `CONTEXTMATRIX_BACKEND_<NAME>_URL`
- `CONTEXTMATRIX_BACKEND_<NAME>_API_KEY`
- `CONTEXTMATRIX_BACKEND_<NAME>_ENABLED`
- `CONTEXTMATRIX_BACKEND_<NAME>_DEFAULT_MODEL`
- `CONTEXTMATRIX_BACKEND_AGENT_RECONCILE_INTERVAL` (agent only)
- `CONTEXTMATRIX_BACKEND_AGENT_AA_API_KEY`, `..._MODEL_ALLOWLIST` (agent only)
- `CONTEXTMATRIX_MCP_API_KEY`, `CONTEXTMATRIX_OP_STORE_DB_PATH`,
  `CONTEXTMATRIX_CHAT_IDLE_TTL`, `CONTEXTMATRIX_CHAT_MAX_CONCURRENT`

Env values win over the file; a backend can be configured through env vars
alone. Unrecognized names or suffixes fail loudly at startup.

### Backend configuration

The task and chat backends carry their own configuration (listener ports,
container image and resource limits, secrets directory, replay-cache sizing,
idle watchdog, orphan sweep, graceful-shutdown drain). Those knobs live in each
backend's `serve.yaml.example` in its own repository, not here. The only fields
that must agree across CM and a backend are the shared `api_key` and the URL CM
uses to reach it.

### Agent backend metrics

The task backend exposes its own Prometheus surface on a loopback admin
listener, HMAC-signed with the backend's `api_key`. Series are namespaced
`cm_agent_*` — webhook request count and duration, container duration by
outcome, a running-containers gauge, callback retries, and log-broadcaster
drops — alongside the standard `go_*` / `process_*` collectors. CM's own
`/metrics` (namespaced `contextmatrix_*`, including
`contextmatrix_backend_webhook_total` for outbound webhook outcomes) is served
on CM's loopback admin listener. Scrape both to cover the full path.

### Per-project (`.board.yaml`)

```yaml
remote_execution:
  worker_image: "my-org/go-worker:latest"
  chat_worker_image: "my-org/go-chat-worker:latest"
```

## Kill Switch Semantics

| Action                       | Scope                | Behavior                                              |
| ---------------------------- | -------------------- | ---------------------------------------------------- |
| Stop (card)                  | Single card          | Kills the container, sets `worker_status: killed`.   |
| Stop All                     | All cards in project | Kills every container for the project.               |
| No task backend enabled      | Global               | Disables all card execution (restart required).      |

## Graceful Shutdown

Each backend handles `SIGTERM` with a structured drain: `/readyz` flips to `503
draining` so load balancers stop routing new webhooks; in-flight requests
finish; the mutating endpoints (`/trigger`, `/message`, `/promote`,
`/end-session`) short-circuit with `503 draining` while `/kill`, `/stop-all`,
`/containers`, `/images`, `/health`, and `/readyz` stay reachable so operators
can still read state and stop work; tracked containers are stopped; and the
process exits
after a bounded drain window. The exact drain timeout and force-cleanup behavior
are backend-internal — see each backend's repository.

## See Also

- `docs/api-reference.md` § Worker & Backend Endpoints, § Chat Endpoints — the
  REST surface the web UI calls.
- `docs/architecture.md` — component responsibilities and the full trust model.
- `docs/agent-workflow.md` — how the in-container worker chooses models, engages
  task skills, and grounds on the repo.
- The `contextmatrix-agent` and `contextmatrix-chat` repositories — backend
  internals and their `serve.yaml.example`.
