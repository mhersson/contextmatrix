# Remote Execution

Remote execution allows autonomous tasks to be triggered from the ContextMatrix
web UI via a **run button** ("Run Auto" or "Run HITL" depending on the
Autonomous mode checkbox). The workflow is executed by a separate binary
(**contextmatrix-runner**) that spawns disposable Docker containers with Claude
Code.

## Architecture Overview

```text
                                   HMAC-signed webhooks
                      ┌──────────────────────────────────────────────────┐
                      │                                                  ▼
  ┌──────────────┐    │    ┌───────────────────┐    ┌────────────────────────┐
  │  Web UI      │────┘    │  contextmatrix    │    │ contextmatrix-runner   │
  │  (Run btn)   │─────────│  (REST API)       │───►│  /trigger              │
  │  (Console)   │◄────────│  (SSE proxy)      │◄───│  /kill  /stop-all      │
  │  (Chat input)│─────────│  POST /message    │───►│  /message              │
  │  (Promote)   │─────────│  POST /promote    │───►│  /promote              │
  │              │         │  (release_card    │───►│  /end-session          │
  │              │         │   → terminal)     │    │                        │
  └──────────────┘         │  POST /mcp        │◄───│  Docker containers     │
                           │  (MCP tools)      │◄───│  (Claude Code)         │
                           └───────────────────┘    └────────────────────────┘
                                  ▲                            │
                                  │      MCP (Bearer auth)     │
                                  └────────────────────────────┘
```

**Message paths:**

- **Run Auto / Run HITL / Stop / Stop All** — trigger/kill/stop-all webhooks from CM to
  runner.
- **Live log streaming** — runner exposes `GET /logs` SSE endpoint; CM proxies
  as `GET /api/runner/logs`. Web UI opens an `EventSource` only while the Runner
  Console panel is open.
- **Chat input** (interactive mode only) — Web UI sends
  `POST /api/runner/message` to CM, which forwards to the runner's `/message`
  endpoint. The runner writes the message to the container's stdin and echoes it
  as a `user` log entry.
- **Promote to autonomous** — Web UI sends `POST /api/runner/promote` to CM,
  which flips the card's `autonomous` flag server-side (git commit + SSE event),
  then forwards to the runner's `/promote` endpoint. The runner calls the
  contextmatrix promote API first (fail closed — returns 502 without stdin write
  on failure), then emits a `system` log entry and injects a canned message into
  stdin telling the agent to check the card at its next gate.

**ContextMatrix** is the coordination layer. It stores cards, manages state, and
sends webhooks to the runner. It never touches code repositories.

**[contextmatrix-runner](https://github.com/mhersson/contextmatrix-runner)** is
a separate binary that:

- Receives trigger/kill webhooks from ContextMatrix
- Spawns disposable Docker containers per task
- Each container runs Claude Code in headless mode
- Claude Code connects back to ContextMatrix via MCP tools

## Webhook Protocol

### Authentication: HMAC-SHA256 Signing

All webhooks are signed using a shared secret configured in both ContextMatrix
(`runner.api_key`) and the runner (`api_key`). The secret is never transmitted
over the wire. The scheme binds the signature to the HTTP method, request
path, timestamp, and body — so a valid signature for one endpoint cannot be
replayed against a different endpoint with an identical body (e.g. `/kill`
and `/end-session`, which both carry `{card_id, project}`). Applies uniformly
to every signed request: POST webhooks, GET `/logs` / `/containers` /
`/autonomous` / `/metrics`, and the runner's status callbacks to CM.

**Signed content:**

```
<METHOD>\n<PATH>\n<TIMESTAMP>.<BODY>
```

- `METHOD`: uppercase HTTP method (`POST`, `GET`)
- `PATH`: request path component — no scheme/host/query (e.g. `/kill`,
  `/api/runner/status`). Sender and receiver MUST agree: any intermediate
  proxy that rewrites paths will cause HMAC auth to fail.
- `TIMESTAMP`: Unix seconds, decimal string
- `BODY`: JSON payload bytes, or empty for GET

**Signing process:**

1. Marshal the JSON payload body (empty for GET).
2. Compute `HMAC-SHA256(shared_secret, METHOD + "\n" + PATH + "\n" + TIMESTAMP + "." + BODY)`.
3. Hex-encode the result.
4. Set headers: `X-Signature-256: sha256=<hex>` and `X-Webhook-Timestamp: <ts>`.

**Verification:** The receiver reads method + path from the incoming HTTP
request, computes the expected HMAC, and compares using constant-time
comparison. It also rejects payloads whose timestamp falls outside the
allowed clock-skew window (5 minutes default).

### ContextMatrix → Runner Webhooks

All requests are `POST` with `Content-Type: application/json`.

#### POST {runner_url}/trigger

Sent when a user clicks "Run Auto" or "Run HITL" on a parent or standalone card.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "repo_url": "git@github.com:org/repo.git",
  "mcp_api_key": "optional-bearer-token",
  "runner_image": "optional/custom-image:latest",
  "base_branch": "develop",
  "interactive": false,
  "model": "claude-sonnet-4-6"
}
```

`model` is always populated by CM from config. When the card's `use_opus_orchestrator` flag is `true`, CM sends `runner.orchestrator_opus_model`; otherwise it sends `runner.orchestrator_sonnet_model`. The runner passes this value to the container as the `CM_ORCHESTRATOR_MODEL` environment variable, which the entrypoint uses as the `--model` argument to Claude Code.

`base_branch` is omitted when not set on the card. When present, the runner
should clone using `-b <base_branch>` and instruct Claude Code to open PRs
against that branch instead of the repository default. See the runner card
CTXRUN-019 for the implementation on the runner side.

`interactive` defaults to `false`. When `true`, the runner sets the
`CM_INTERACTIVE=1` container environment variable and launches Claude Code with
`--input-format stream-json --output-format stream-json`. After attaching stdin,
the runner writes a priming stream-json user message to the container that
instructs Claude to start the `create-plan` workflow immediately — no human
input is required to begin. See [Interactive Mode](#interactive-mode) for
details.

**Note:** `feature_branch` and `create_pr` are auto-enabled on the card for
**all** run triggers — both autonomous and HITL runs. This ensures a
feature branch and PR are always created regardless of the execution mode chosen
at launch.

## Task skills mount

The runner bind-mounts `task_skills_dir` (configured per runner host) read-only into worker containers at `/host-skills`. The entrypoint copies the resolved subset of skills into `~/.claude/skills/` for Claude Code to discover.

### Trigger payload field

CM resolves the skill list (per `docs/data-model.md`) and ships it in the `/trigger` payload:

```json
{
  "card_id": "ALPHA-001",
  "project": "alpha",
  "repo_url": "...",
  "task_skills": ["go-development", "documentation"]
}
```

`task_skills` is omitted when the resolved list is `nil` (mount full set). Empty array means "explicit none — mount nothing."

### Runner env vars

The runner forwards the resolution to the container as:

- `CM_TASK_SKILLS_SET=1` whenever the payload had a non-nil `task_skills` field.
- `CM_TASK_SKILLS=<csv>` containing the comma-joined list (empty allowed).

`CM_TASK_SKILLS_SET` distinguishes "unset → mount all" from "set-but-empty → mount nothing" — the env var alone isn't enough because `unset` and `empty string` are indistinguishable in shell.

### Entrypoint behaviour

```
if /host-skills exists:
    if CM_TASK_SKILLS_SET set:
        for each name in CM_TASK_SKILLS:
            validate name
            cp -r /host-skills/<name> ~/.claude/skills/
            (skip with warning if name not present)
    else:
        cp -r /host-skills/*/ ~/.claude/skills/
```

### On-trigger pull

Before constructing the container config, the runner runs `git pull --ff-only` on `task_skills_dir`. On failure, the runner logs and continues with the existing local clone — the trigger never aborts because of a sync issue.

### Required tool

The container's `--allowed-tools` allowlist must include `Skill` for Claude Code's native skill engagement to work.

#### POST {runner_url}/kill

Sent when a user clicks "Stop" on a running card, or by the end-session
subscriber / reconcile sweep when a card reaches a terminal state.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project"
}
```

**Idempotent.** Three server-side cases:

1. **Tracker has the entry** → normal cancel path: cancel the run's context,
   let `waitAndCleanup` run its defers (`docker stop` + `docker rm -f`).
   Returns `200 {"ok": true, "message": "container killed"}`.
2. **Tracker is empty but Docker still has a labeled container** for
   `(project, card_id)` → the runner reaches past the tracker via
   `docker rm -f` on every matching container. Returns `200 {"ok": true,
   "message": "force-removed"}`. This closes the tracker/Docker divergence
   case where a prior cleanup cleared the tracker entry before Docker
   removal succeeded; without this fallback the container leaks to
   `container_timeout` (default 2h).
3. **Neither tracker nor Docker has a matching container** → true no-op.
   Returns `200 {"ok": true, "message": "no-op (already stopped)"}`.

All three return `200` so CM's retry logic doesn't need to distinguish
"not found" from "killed".

#### GET {runner_url}/containers

Returns every Docker container labeled `contextmatrix.runner=true` on the
runner, regardless of running / exited state. HMAC-signed (the timestamp
covers an empty body).

Consumed by CM's reconcile sweep as the authoritative answer to "what
containers are actually running right now" — independent of the runner's
in-memory tracker and of CM's card-level `runner_status` field. The sweep
correlates each entry against CM's card store and kills anything whose card
is terminal, missing, or exceeds the max-age cap. See
`internal/runner/reconcile.go` for the decision rules.

```json
{
  "ok": true,
  "containers": [
    {
      "container_id": "778fe6561d75abc...",
      "container_name": "cmr-contextmatrix-ctxmax-436",
      "card_id": "ctxmax-436",
      "project": "contextmatrix",
      "state": "running",
      "started_at": "2026-04-23T10:30:00Z",
      "tracked": false
    }
  ]
}
```

`tracked` reflects the runner's in-memory tracker state at response time.
The signature of the divergence bug the sweep is designed to catch is
`tracked: false` combined with `state: "running"` — a container Docker is
running that the runner's own tracker has already forgotten about.

**Error responses:**

| Status | Condition                                |
| ------ | ---------------------------------------- |
| 401    | Missing / invalid HMAC signature         |
| 502    | Docker daemon unreachable (`upstream_failure`) |

#### POST {runner_url}/stop-all

Sent when a user clicks "Stop All" in the header.

```json
{
  "project": "my-project"
}
```

#### POST {runner_url}/message

Sent when a user submits a chat message while a container is running in
interactive mode. HMAC-signed identically to trigger/kill.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "message_id": "msg-uuid-1234",
  "content": "Please focus on the authentication module first."
}
```

The runner:

1. Looks up the running container for `card_id` / `project`.
2. Writes the following newline-terminated JSON to the container's stdin:
   ```json
   {
     "type": "user",
     "message": {
       "role": "user",
       "content": [{ "type": "text", "text": "<content>" }]
     }
   }
   ```
3. Emits a broadcaster `LogEntry` of type `user` (see
   [LogEntry types](#logentry-types)) so the browser sees the message echoed in
   the console.

**Error responses:**

| Status | Condition                                |
| ------ | ---------------------------------------- |
| 404    | Container not tracked (card not running) |
| 409    | Container is not in interactive mode     |
| 413    | `content` exceeds 8 KiB                  |

#### POST {runner_url}/promote

Sent when a user clicks "Switch to Autonomous" while a container is running in
interactive mode. HMAC-signed identically to trigger/kill.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project"
}
```

The runner performs a two-step operation in strict order:

1. **Verify the autonomous flag (fail closed):** Calls
   `GET {contextmatrix_url}/api/v1/cards/{project}/{id}/autonomous` and checks
   that the response body `{"autonomous": bool}` is `true`. CM already flipped
   the flag before sending this webhook, so the GET is a read-only
   confirmation. The request is HMAC-SHA256-signed under the standard method/path/timestamp
   scheme (empty body) using `runner.api_key`. If the call fails (network error,
   non-2xx) or `autonomous` is not `true`, the runner returns 502 and does
   **not** write to stdin — the card remains in interactive mode.
2. **Inject the canned stdin message:** Emits a `system` `LogEntry` with content
   `"promoted to autonomous mode"`, then writes a stream-json user message to
   the container's stdin:

   > "Autonomous mode has been enabled (card flag flipped). Check the card with
   > `get_card` at your next gate and continue on the autonomous branch. Do not
   > wait for further user input."

   The agent at its next HITL gate calls `get_card`, sees `autonomous: true`,
   and skips the gate automatically. No stdin message is written on API failure.

3. **Close stdin:** Immediately after the canned message is written, the runner
   closes the container's stdin. This signals EOF to `claude
   --input-format stream-json`, which causes the process to finish any in-flight
   work and exit cleanly through the normal `waitAndCleanup` path — without
   waiting for `container_timeout`. An already-closed stdin (e.g. a racing
   `/end-session`) is treated as non-fatal: a warning is logged and the endpoint
   still returns 200.

**Error responses:**

| Status | Condition                                            |
| ------ | ---------------------------------------------------- |
| 404    | No container tracked for this card                   |
| 409    | Container is not in interactive mode                 |
| 502    | ContextMatrix card verification failed (fail closed) |

#### POST {runner_url}/end-session

Sent by ContextMatrix when a card tied to an interactive container reaches a
terminal state (`done` or `not_planned`) and is released. Closes the
container's stdin so `claude`, running with `--input-format stream-json`,
receives EOF and exits; the container then terminates through the normal
`waitAndCleanup` path. HMAC-signed identically to `/kill`.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project"
}
```

Sent by an event-bus subscriber in CM that listens for `card.released` and
`card.state_changed`. The subscriber fires only when both hold:

1. `card.assigned_agent` is empty (the card has actually been released).
2. `card.state` is `done` or `not_planned`.

`card.runner_status` is intentionally NOT part of this predicate. An earlier
version gated on `runner_status ∈ {queued, running}`, which silently skipped
any container whose `runner_status` had drifted away from Docker reality
(the runner's `reportCompleted` / `reportFailure` callbacks flip the field
before the Docker cleanup defers actually succeed). Gating on it turned every
such drift into a permanent leak. The runner's `/kill` is idempotent, so the
subscriber firing "spuriously" against an already-dead container costs one
200 no-op and eliminates that class of bug at the source.

This prevents the container from exiting on intermediate `release_card` calls
an orchestrator makes between subtasks: a release without a terminal-state
transition does not satisfy the predicate.

**End-session is always followed by `/kill`.** Claude in `--input-format
stream-json` mode has been observed keeping the container process alive well
past stdin EOF (processing in-flight work and then idling instead of exiting).
The subscriber therefore calls `/end-session` first (polite close, lets claude
exit gracefully if it respects EOF) and then calls `/kill` unconditionally as
a safety net so a terminal-state card never leaves a live container behind.
`/kill` is idempotent — if the container is already gone the runner returns
200 no-op. Expected `/end-session` responses (409 no stdin attached for
autonomous containers, 410 stdin already closed by an earlier `/promote`) are
classified as normal and suppressed from the warning log; `/kill` still fires.

The runner emits a `system` `LogEntry` with content
`"session ended (stdin closed)"` before returning.

**Error responses:**

| Status | Condition                                                      |
| ------ | -------------------------------------------------------------- |
| 404    | No container tracked for this card                             |
| 409    | Container is not in interactive mode, or stdin already closed  |

### Runner → ContextMatrix: SSE Log Stream

#### GET {runner_url}/logs

Streams live log entries via Server-Sent Events. Used by the ContextMatrix proxy
endpoint — not called directly by the browser.

**Authentication:** HMAC-signed GET request. The body is empty; the signature
covers `timestamp.""` (timestamp concatenated with empty body).

Required headers:

```
X-Signature-256: sha256=<hex>
X-Webhook-Timestamp: <unix-timestamp>
```

**Query parameter:** `?project=<name>` — filters entries to a single project.
Omit to receive entries from all projects.

**Response:** `Content-Type: text/event-stream`. Each event is a JSON-encoded
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

`type` values:

<a name="logentry-types"></a>

| type        | Source             | Meaning                                                           |
| ----------- | ------------------ | ----------------------------------------------------------------- |
| `text`      | Claude Code stdout | Parsed assistant text block                                       |
| `thinking`  | Claude Code stdout | Parsed assistant thinking block                                   |
| `tool_call` | Claude Code stdout | Non-MCP tool call: `Name: <summary>`, truncated to 200 runes with `…` |
| `stderr`    | Container stderr   | Raw stderr line from the container                                |
| `system`    | Runner lifecycle   | Container lifecycle events (started, completed, failed, canceled) |
| `user`      | Chat input         | User message submitted via the chat input                         |

**Keepalive:** The runner sends `: keepalive\n\n` comments every 15 seconds to
prevent proxy and browser timeouts.

**Secret redaction:** The log parser redacts common credential patterns (GitHub
tokens, Anthropic API keys, Bearer tokens) before publishing. Secrets are
replaced with `[REDACTED]`.

### Runner → ContextMatrix Callback

#### POST /api/runner/status

The runner reports container lifecycle events back to ContextMatrix. Must
include `X-Signature-256` header signed with the shared secret.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "runner_status": "running",
  "message": "container started"
}
```

Valid `runner_status` values: `"running"`, `"failed"`, `"completed"`.

Task completion is **not** reported via this endpoint — the Claude Code instance
inside the container uses the normal MCP `complete_task` tool.

#### GET /api/v1/cards/{project}/{id}/autonomous

Read-only endpoint called by the runner during `/promote` to fail-closed
confirm the card's autonomous flag before writing the canned stdin message.
Returns:

```json
{"autonomous": true}
```

**Authentication:** HMAC-SHA256 signature under the standard
method/path/timestamp scheme (empty body) with the shared runner secret.
Headers:

- `X-Signature-256: sha256=<hex>`
- `X-Webhook-Timestamp: <unix-seconds>`

Only registered when the runner is enabled on the CM side
(`runner.enabled: true`). Missing / invalid HMAC returns 403; unknown card
returns 404.

### Response Format

All webhook endpoints return:

```json
{
  "ok": true,
  "message": "optional success message"
}
```

Or on error:

```json
{
  "ok": false,
  "code": "error_code",
  "message": "human-readable description"
}
```

**Stable error codes:**

| Code               | Meaning                                                     |
| ------------------ | ----------------------------------------------------------- |
| `invalid_json`     | Request body could not be parsed as JSON                    |
| `invalid_field`    | A required field is missing or has an invalid value         |
| `unauthorized`     | HMAC signature missing, invalid, or timestamp out of window |
| `not_found`        | Referenced card or container does not exist                 |
| `conflict`         | Operation not valid in the current state                    |
| `duplicate`        | A duplicate resource was detected                           |
| `stdin_closed`     | Container stdin is closed; no further messages accepted     |
| `too_large`        | Request payload exceeds the size limit (e.g., 8 KiB)       |
| `limit_reached`    | A configured capacity limit has been reached                |
| `internal`         | Unexpected runner-side error                                |
| `upstream_failure` | Runner could not reach an upstream dependency (e.g., CM)   |
| `draining`         | Runner is shutting down and not accepting new work          |

**Per-endpoint behaviour notes:**

- **`/stop-all`** — may return `207 Multi-Status` when some containers were
  stopped and others failed. The body contains per-card results.
- **`/kill`** — idempotent: returns `200` (not 404) when the card is not
  tracked. Use this to safely call stop on cards that may already be finished.
- **`/message`** — may return `410` with code `stdin_closed` after the
  container session has ended and stdin is no longer writable.
- **`/promote`** — closes stdin immediately after writing the canned message.
  A subsequent `/end-session` on the same card is idempotent (returns `409`
  because stdin is already closed).
- **All mutating endpoints** — return `503` with code `draining` while the
  runner is performing a graceful shutdown. Clients should not retry during
  a draining window.

### Retry Policy

ContextMatrix retries failed webhooks with exponential backoff:

- 3 attempts total (1s, 2s, 4s delays)
- Only retries on network errors and HTTP 5xx responses
- HTTP 4xx responses fail immediately (no retry)
- Per-request timeout: 10 seconds

## Container Lifecycle

1. Runner receives `/trigger` webhook
2. Pulls Docker image (base image or per-project override from `runner_image`)
3. Starts container with:
   - Claude Code CLI pre-installed
   - Secrets delivered via tmpfs bind-mount at `/run/cm-secrets/env` (not plain
     environment variables) — the entrypoint sources this file at startup
   - `--allowed-tools` with an explicit tool allowlist replaces
     `--dangerously-skip-permissions`. Two allowlist arrays are defined:
     - `ALLOWED_TOOLS_COMMON` — used for both HITL and autonomous runs
     - `ALLOWED_TOOLS_AUTO_EXTRAS` — adds `Task` on top of `ALLOWED_TOOLS_COMMON`
       for fully autonomous (non-interactive) runs
   - Git credentials mounted (not baked into image)
   - The card ID and project name passed as arguments
   - Injected environment variables include:
     - `CM_MCP_URL` — ContextMatrix MCP endpoint
     - `CM_MCP_API_KEY` — Bearer token for MCP authentication
     - `CM_CARD_ID` / `CM_PROJECT` — card being executed
     - `CM_ORCHESTRATOR_MODEL` — model name from the trigger payload
     - `CM_INTERACTIVE` — `1` for HITL mode, unset or `0` for autonomous
     - `CM_CORRELATION_ID` — opaque ID propagated from the `/trigger` request's
       `X-Correlation-ID` header; the container forwards it as `X-Correlation-ID`
       on all outbound requests for end-to-end tracing
4. Claude Code runs the `run-autonomous` workflow:
   - Connects to ContextMatrix via MCP
   - Claims the card
   - Clones the repo from `repo_url`
   - Plans, executes, documents, reviews
   - Creates feature branch and PR
   - Completes the card via MCP `complete_task`
5. Container exits after workflow completes
6. Runner cleans up the container

**On kill:** Container is destroyed immediately. All uncommitted work is
discarded. No partial saves.

### Terminal-state cleanup (HITL containers)

A HITL container's `claude` process does not exit when its stdin is closed —
in stream-json mode it treats EOF as "no more user input for now" and keeps
running. A card that reaches a terminal state (`done` or `not_planned`) and
is released must therefore be killed by ContextMatrix explicitly, otherwise
the container would leak until the runner's `container_timeout` (default 2h).

Two independent mechanisms guarantee this cleanup, and they now use
different truths so a bug in either cannot silently hide a live container:

1. **Event subscriber (fast path).** `internal/runner/endsession.go` watches
   the event bus for `card.released` and `card.state_changed`. When it sees a
   card with `state ∈ {done, not_planned}` + `assigned_agent == ""`, it fires
   `/end-session` followed by `/kill` against the runner. Typical latency:
   tens of milliseconds. `runner_status` is intentionally not consulted —
   see the end-session section above for why.
2. **Reconcile sweep (Docker-authoritative backstop).**
   `internal/runner/reconcile.go` runs every `runner.reconcile_interval`
   (default **60s**). Every tick it calls `GET /containers` on the runner
   and, for each container Docker is actually running, looks up the card
   and kills the container when:
   - the card is missing (deleted or renamed), or
   - the card's state is `done` / `not_planned`, or
   - the container has been alive longer than `ContainerMaxAge` (150m).

   **The sweep does not read `runner_status`.** It reasons exclusively from
   two authoritative sources — Docker ("is this container running?") and
   the card store ("should it be?"). Every previous implementation of this
   sweep consulted CM's own `runner_status` bookkeeping and inherited the
   drift bug where a failed cleanup defer flipped the field to `completed`
   / `failed` while Docker still had the container, then every subsequent
   sweep silently skipped it. That class of bug is now unreachable.

The event path's logline is `"end-session + kill sent" source=subscriber`;
the sweep's is `"reconcile sweep killing container" ... reason=<terminal_state
|no_such_card|age_cap>` followed by the same `source=sweep` kill log. Every
sweep tick also emits a `"reconcile sweep tick"` line with `scanned` and
`killed` counts, so "is the sweep actually running?" is answerable from a
single grep.

Setting `reconcile_interval` to `"0s"` disables the sweep. Not recommended
outside of tests — the event path is best-effort (events.Bus drops events on
subscriber buffer overflow, and events published during CM restart are never
delivered), so without the sweep a single dropped event can leak a container
for the full `container_timeout` window.

## Worker Safety

### Idle-Output Watchdog

The runner monitors each container for output activity. If a container produces
no stdout or stderr output for longer than `idle_output_timeout` (default: **30
minutes**), the runner treats it as hung and force-kills the container. This
prevents silent deadlocks — e.g., a Claude Code process blocked waiting for
user input that will never arrive in autonomous mode — from consuming slots and
leaving cards permanently `running`.

The watchdog timer resets every time the container emits any output line. A
container that is actively working but producing slow output (e.g., a long
compile step with no progress lines) will not be killed as long as it emits at
least one line within the window.

### Maintenance Loop

A background maintenance goroutine runs every `maintenance_interval` (default:
**10 minutes**) and performs two cleanup tasks:

1. **Orphan sweep** — scans running Docker containers for any that were started
   by this runner but are no longer tracked in the in-memory container map (e.g.,
   because the runner restarted mid-session). Orphaned containers are stopped and
   their log sessions terminated.
2. **Image pruning** — removes dangling Docker images (untagged intermediate
   layers) that are older than 24 hours. This prevents unbounded disk growth
   from repeated image pulls and rebuilds.

Both tasks are best-effort: errors are logged but do not stop the maintenance
loop. The maintenance loop is started automatically when the runner starts and
stopped gracefully on shutdown.

## Security Model

### Webhook Signing (HMAC)

A single shared secret (`runner.api_key` / `api_key`) authenticates all
directions:

- ContextMatrix signs outbound webhooks to the runner (trigger, kill, stop-all)
- ContextMatrix signs the SSE log proxy request to the runner (`GET /logs`)
- Runner signs status callbacks to ContextMatrix
- Uses HMAC-SHA256 — the secret never travels over the wire

For the `GET /logs` request the body is empty, so the signature covers
`timestamp.""` (timestamp bytes concatenated with empty body bytes).

### MCP Authentication (Bearer Token)

Optional but recommended. When `mcp_api_key` is set in ContextMatrix config:

- All MCP requests must include `Authorization: Bearer <key>`
- The key is passed to the runner in the trigger payload
- Runner injects it into the container's CC MCP configuration

### Human-Only Controls

- Only humans (no `X-Agent-ID` header or `human:*` prefix) can:
  - Click "Run Auto" / "Run HITL" (trigger remote execution)
  - Click "Stop" (kill a running container)
  - Click "Stop All" (kill all containers for a project)
  - Set `autonomous`, `feature_branch`, `create_pr` flags
  - Send chat messages to an interactive container
  - Promote an interactive container to autonomous mode
- Agents inside containers cannot escalate themselves to autonomous mode

### Per-Project Kill Switch

Each project can override the global runner setting:

```yaml
# In .board.yaml
remote_execution:
  enabled: true # or false to disable for this project
  runner_image: "custom/image:v2" # optional per-project Docker image
```

Resolution order:

1. Project's `remote_execution.enabled` (if set)
2. Global `runner.enabled` (fallback)

**API responses reflect the effective state.** `GET /api/projects` and
`GET /api/projects/{project}` always return `remote_execution.enabled` as the
resolved value — global disabled overrides any per-project setting. Clients do
not need to consult the global config separately; the response value is
authoritative for whether the run button should be enabled.

### Global Kill Switch

Set `runner.enabled: false` in `config.yaml` to disable remote execution
entirely. The run button will not appear in the UI, and trigger endpoints
return 503.

## Interactive Mode

When `interactive: true` is included in the `/trigger` payload, the runner
starts the container in Human-in-the-Loop (HITL) mode.

### Container Environment

The runner sets `CM_INTERACTIVE=1` in the container's environment. The
`entrypoint.sh` script branches on this variable:

- **`CM_INTERACTIVE` unset or `0`** — normal autonomous mode: Claude Code is
  invoked with `--output-format stream-json` and the workflow proceeds
  automatically.
- **`CM_INTERACTIVE=1`** — interactive mode: Claude Code is invoked with
  `--input-format stream-json --output-format stream-json` and a minimal
  system-context hint as the `-p` prompt. After attaching stdin and registering
  the writer with the tracker, the runner writes a priming stream-json user
  message (built via `streammsg.BuildUserMessage`) directly into the container's
  stdin. The priming message instructs Claude to call
  `get_skill(skill_name='create-plan', ...)` immediately, so plan drafting
  starts without waiting for user input. The user provides approval at the
  skill's built-in gates (plan approval, subtask execution decision, review) via
  the chat input.

### Message Flow

```
Web UI (chat input) → CM POST /api/runner/message
                     → Runner POST /message
                     → container stdin (stream-json user message)
                     → Runner LogEntry{type: "user"}  (echoed to browser)

Web UI (promote btn) → CM POST /api/runner/promote
                      → CM flips card autonomous=true (git commit + SSE event)
                      → Runner POST /promote
                      → Runner GET /api/v1/cards/{project}/{id}/autonomous (HMAC-signed;
                                    502+stop if autonomous != true or request fails)
                      → container stdin (canned autonomous-mode message)
                      → Runner LogEntry{type: "system", content: "promoted to autonomous mode"}
                      → Runner closes container stdin (EOF → container exits after work done)
```

### Feature Branch Flags

`feature_branch` and `create_pr` are auto-enabled on the card whenever a run
is triggered — for both autonomous and HITL runs. The `/promote` endpoint
additionally sets `autonomous: true` when the user switches a running
interactive session to autonomous mode.

## Log Streaming Architecture

The live log pipeline has three layers:

### Runner: `internal/logbroadcast`

`Broadcaster` is a thread-safe fan-out hub. It manages a set of subscribers,
each with a buffered channel (256 entries). `Publish(LogEntry)` is non-blocking:
if a subscriber's buffer is full the entry is dropped and a warning logged (slow
subscriber protection).

`Subscribe(project string)` returns a `(<-chan LogEntry, unsubscribe func())`.
Pass an empty string to receive all projects.

Sources that call `Publish`:

- **`container.Manager`** — emits `system` entries for container lifecycle
  events (started, completed, failed, canceled, timed-out) and `stderr` entries
  for each container stderr line.
- **`logparser.ProcessStream`** — emits `text`, `thinking`, and `tool_call`
  entries parsed from Claude Code's `--output-format stream-json` stdout. The
  caller (container manager) pre-fills `card_id` and `project` on each entry
  before publishing.

### Runner: `GET /logs` SSE Endpoint

`webhook.Handler.handleLogs` subscribes to the broadcaster, then streams entries
as `data: {json}\n\n` SSE events. It sends `: keepalive\n\n` comments every 15
seconds. The write deadline is cleared on the underlying connection to allow
long-lived connections past the server's `WriteTimeout`.

### ContextMatrix: `GET /api/runner/logs` — Two Modes

`api.runnerHandlers.streamRunnerLogs` handles both code paths, selected by the
`card_id` query parameter:

Both paths set `X-Accel-Buffering: no` on the response and write a
`: keepalive\n\n` SSE comment every 30 seconds per subscription to survive
Cloudflare/nginx idle timeouts (~100 s).

**SSE payload shapes:**

Normal events:
```json
{"type":"text","content":"...","card_id":"PROJ-042","ts":"...","seq":42}
```

Marker frames:
- `{"type":"terminal","seq":N}` — session ended; client should stop reconnecting.
- `{"type":"dropped","seq":N,"count":N}` — ring-buffer overflowed; `count` events evicted.

**Card-scoped path** (`?project=P&card_id=X`):

1. Delegates to the [Session Log Manager](#session-log-manager).
2. Calls `manager.Subscribe(cardID)`, which atomically captures a snapshot of
   all buffered events and registers a live-event channel.
3. Sends SSE headers and flushes immediately.
4. Delivers the snapshot events first (replay), then tails the live channel.
5. On `terminal` event or channel close, ends the response.
6. On browser disconnect (`r.Context().Done()`), calls the unsubscribe func.

If the session manager is not initialised (runner disabled), returns 204.

**Project-scoped path** (`?project=P`, no `card_id`):

Used by the Runner Console panel (`ProjectShell`). Backed by the same Session
Log Manager as the card-scoped path. On each client connection, calls
`manager.StartProject(project)` (idempotent) to open a single long-lived
upstream SSE connection that accepts all events for the project. Calls
`manager.SubscribeProject(project)`, which replays the buffered snapshot first
and then tails live events — identical snapshot-before-live ordering guarantee
as the card-scoped path. Reconnecting clients receive all events buffered since
the first connect. The session key is namespaced as `"project:<name>"` in the
shared Manager maps so it cannot collide with card IDs. Cleanup is handled by
the idle TTL sweeper (2 h default); no explicit stop is needed.

If the session manager is not initialised (runner disabled), returns 204.

Both paths clear the write deadline via `http.ResponseController` before
entering the streaming loop (see `docs/gotchas.md` § SSE and WriteTimeout). The
endpoint is only registered when `runner != nil`.

### Session Log Manager

`internal/runner/sessionlog.Manager` is the server-side buffering and fan-out
layer that fixes the reconnect-loses-log-history bug.

#### Responsibilities

- **One upstream connection per card**: on
  `manager.Start(ctx, cardID, project)`, opens a single long-lived HMAC-signed
  SSE connection to `{runner_url}/logs?project=P`, parses events, and writes
  them into the per-card ring buffer. Events for other cards are filtered out
  before buffering (the runner streams all project events on the same
  connection).
- **Snapshot + live fan-out**: `Subscribe(cardID)` returns a
  `(<-chan Event, unsub)`. The snapshot of all buffered events is delivered
  first (replay), then live events follow. Multiple browser tabs can subscribe
  concurrently.
- **Project-scoped sessions**: `StartProject(ctx, project)` / `StopProject(project)` /
  `SubscribeProject(project)` mirror the card-scoped API but buffer all events
  for the project (no card-ID filter). The internal session key is
  `"project:<name>"`, which cannot collide with card IDs in the shared maps.
  `StartProject` is called by `streamProjectSession` on each client connect
  (idempotent); cleanup is handled by the idle sweeper. Project-scoped events
  include a populated `CardID` field on `sessionlog.Event` so the frontend
  card-ID filter dropdown keeps working.
- **Bounded ring buffer**: each session (card-scoped or project-scoped) enforces
  dual caps — 2000 events OR 1 MiB total payload, whichever is reached first.
  On overflow, the oldest events are dropped and a single synthetic `dropped`
  marker event is inserted/updated at the front of the buffer.
- **Lifecycle**: `Start` is called by `CardService.UpdateRunnerStatus` on
  `→running`. `Stop` is called (fire-and-forget, never fails the status update)
  on transition to any terminal status (`failed`, `killed`, `completed`). Stop
  cancels the upstream connection, sends a `terminal` event to all subscribers,
  and clears the buffer.
- **Upstream retry**: on read error the pump retries with exponential backoff
  (250 ms base, 4 s cap, 5 attempts). The `attempt` counter resets to 0
  whenever a frame is successfully delivered, so transient disconnects during
  a long-running session do not accumulate toward the permanent-failure limit —
  a session can tolerate arbitrarily many brief disconnects as long as each
  reconnection delivers at least one frame. After all retries are exhausted
  without a successful frame the session is marked permanently failed: all
  active and pending subscribers receive a `terminal` event and their channels
  are closed. Any subsequent `Subscribe` call for that card takes a fast path —
  it receives an immediate `terminal` event without parking — until `Start` is
  called again (which clears the failure flag).
- **Slow-subscriber drops**: the fan-out loop is non-blocking. If a subscriber's
  channel (256-entry buffer) is full, the event is dropped. Each drop:
  increments `Manager.DroppedEvents()` (an atomic counter, monotonically
  increasing across all sessions); emits a single `slog.Warn` per fan-out pass
  (throttled to one per second globally); and sends a synthetic
  `{type: "dropped", payload: nil}` marker into the subscriber channel. The
  `nil` payload distinguishes this fan-out drop marker from the ring-buffer
  eviction marker (which encodes a drop count as an 8-byte little-endian
  payload). `DroppedEvents()` is available for future Prometheus export.
- **Session cap**: default 64 concurrent sessions (card-scoped and
  project-scoped combined); `Start`/`StartProject` return an error if the cap is
  reached.
- **Idle sweeper**: `StartSweeper(ctx)` runs a background goroutine that
  force-closes sessions running longer than the TTL (default 2 h) without an
  explicit Stop. Sweeps at TTL/2 intervals.

#### Defaults and configuration knobs

| Knob                   | Default | Option               |
| ---------------------- | ------- | -------------------- |
| Per-session event cap  | 2000    | `WithMaxEvents(n)`   |
| Per-session byte cap   | 1 MiB   | `WithMaxBytes(n)`    |
| Concurrent session cap | 64      | `WithMaxSessions(n)` |
| Idle session TTL       | 2 h     | `WithSessionTTL(d)`  |

All defaults are defined as constants in `internal/runner/sessionlog/buffer.go`
(`DefaultMaxEvents`, `DefaultMaxBytes`) and `manager.go` (`DefaultMaxSessions`,
`DefaultSessionTTL`). The values used at startup are hardcoded in
`cmd/contextmatrix/main.go`; they are not exposed in `config.yaml`.

### Frontend: On-demand connection

The `EventSource` is opened only while the Runner Console panel is visible.
`useRunnerLogs({ enabled })` connects on `enabled=true` and disconnects on
`enabled=false` or component unmount. This satisfies the requirement that no log
traffic flows when the console is closed.

When `cardId` is passed, the hook connects to the card-scoped endpoint
(`?project=P&card_id=X`). The server delivers the buffered snapshot first, so a
client that reconnects after a gap receives all previous events including any
pending HITL questions. The client-side ring buffer (`maxEntries`, default 5000)
still applies on top of the server snapshot.

## Configuration Reference

### ContextMatrix (`config.yaml`)

```yaml
# MCP endpoint authentication (optional)
mcp_api_key: "your-bearer-token"

# Runner integration
runner:
  enabled: false
  url: "http://localhost:9090" # Runner base URL
  api_key: "shared-hmac-secret" # HMAC signing key (min 32 chars)
  orchestrator_sonnet_model: "claude-sonnet-4-6" # Model sent when use_opus_orchestrator is false
  orchestrator_opus_model: "claude-opus-4-7"     # Model sent when use_opus_orchestrator is true
```

Environment variable overrides:

- `CONTEXTMATRIX_MCP_API_KEY`
- `CONTEXTMATRIX_RUNNER_ENABLED`
- `CONTEXTMATRIX_RUNNER_URL`
- `CONTEXTMATRIX_RUNNER_API_KEY`
- `CONTEXTMATRIX_RUNNER_ORCHESTRATOR_SONNET_MODEL`
- `CONTEXTMATRIX_RUNNER_ORCHESTRATOR_OPUS_MODEL`

### Runner (`config.yaml` — reference for runner implementor)

```yaml
# ContextMatrix connection
contextmatrix_url: "http://contextmatrix:8080"
# container_contextmatrix_url: "http://host.docker.internal:8080"  # Override when containers need a different address
api_key: "shared-hmac-secret" # Must match CM's runner.api_key

# HTTP ports
# The main port serves public endpoints (/trigger, /readyz, /logs, etc.)
# The admin_port serves privileged endpoints (/metrics, /ready) and is bound
# to 127.0.0.1 only. Default: 9091
admin_port: 9091

# Logging
log_format: "json" # "json" or "text"

# Container defaults
# base_image must be digest-pinned (e.g. "my-org/runner@sha256:abc123...")
# to guarantee reproducible, tamper-proof execution environments.
base_image: "contextmatrix/runner@sha256:<digest>"
max_concurrent: 3    # Max simultaneous containers
container_timeout: "2h" # Force-kill after this duration

# Resource limits applied to every spawned container
container_memory_limit: "4g"  # Docker memory limit (e.g. "512m", "4g")
container_pids_limit: 512     # Docker PIDs limit

# Secrets directory — files here are mounted read-only into containers
secrets_dir: "/run/secrets/runner"

# Allowed Docker images — if non-empty, the runner rejects trigger payloads
# that request an image not on this list. Use digest-pinned refs.
allowed_images: []

# Replay-attack prevention for HMAC-signed webhooks
webhook_replay_cache_size: 1024  # Number of recent webhook IDs to cache
webhook_replay_skew_seconds: 30  # Allowed clock skew for timestamp validation

# Stdin deduplication — prevents the same message being injected twice on retry
message_dedup_cache_size: 512  # Number of recent message IDs to cache
message_dedup_ttl_seconds: 300 # TTL for each dedup cache entry (seconds)

# Idle watchdog — kills a container that emits no stdout/stderr within this window
idle_output_timeout: "30m"

# Maintenance loop — how often the runner sweeps for orphaned containers and
# prunes stale images
maintenance_interval: "10m"

# HMAC-sign the GET /autonomous verify check sent to ContextMatrix
use_hmac_for_verify_autonomous: true

# GitHub auth — covers all git operations inside worker containers.
# auth_mode: "app" (recommended) or "pat".
# For end-to-end setup see docs/github-auth-setup.md.
github:
  auth_mode: "app"  # CMR_GITHUB_AUTH_MODE
  host: ""          # CMR_GITHUB_HOST — GHE/GHEC-DR hostname; empty for github.com
  api_base_url: ""  # CMR_GITHUB_API_BASE_URL — override for non-standard enterprise URLs

  # GitHub App credentials (required when auth_mode is "app")
  app:
    app_id: 0            # CMR_GITHUB_APP_ID
    installation_id: 0   # CMR_GITHUB_INSTALLATION_ID
    private_key_path: "" # CMR_GITHUB_PRIVATE_KEY_PATH

  # Fine-grained PAT (required when auth_mode is "pat")
  pat:
    token: "" # CMR_GITHUB_PAT_TOKEN
```

For end-to-end auth setup, see
[`docs/github-auth-setup.md`](github-auth-setup.md). The runner uses
the same model: a single identity (`app` with App credentials, or `pat`
with a fine-grained PAT) covers all git operations inside worker containers.

When using GitHub Enterprise, set `github.host` (and optionally
`github.api_base_url`) in both ContextMatrix and the runner to target
the same enterprise instance. The runner entrypoint derives the git host
automatically from the repo URL sent in the trigger payload, so no
additional git configuration is needed.

### Operator Endpoints

The runner exposes two categories of endpoints:

**Public port** (configured by the main listener, default `:9090`):

| Endpoint     | Auth        | Description                                                                                                                                                         |
| ------------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /readyz` | None        | Health probe. Returns `200 {"ok":true}` when the runner is ready to accept work. Returns `503 {"ok":false,"reason":"preflight"}` during startup warmup, and `503 {"ok":false,"reason":"draining"}` during graceful shutdown. Used by load balancers and Kubernetes liveness/readiness probes. |

**Admin port** (`admin_port`, default `127.0.0.1:9091` — loopback only):

| Endpoint       | Auth           | Description                                                                                                                    |
| -------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `GET /metrics` | HMAC-signed    | Prometheus metrics endpoint. Uses the same HMAC key as webhook signing (`api_key`). Only accessible on the loopback interface. |
| `GET /ready`   | None (loopback) | Unauthenticated readiness probe. Identical semantics to `/readyz` but restricted to the admin port (loopback only).           |

### Per-Project (`.board.yaml`)

```yaml
remote_execution:
  enabled: true
  runner_image: "my-org/go-runner:latest"
```

## Card Runner Status

The `runner_status` field tracks the container lifecycle independently of the
card's workflow state:

| runner_status | Meaning                                                                       |
| ------------- | ----------------------------------------------------------------------------- |
| (empty)       | Not associated with runner                                                    |
| `queued`      | Trigger webhook sent, container not yet started                               |
| `running`     | Container is running, CC is working                                           |
| `failed`      | Container crashed or webhook failed                                           |
| `killed`      | User stopped the task                                                         |
| `completed`   | Container finished successfully (transient — cleared on transition to `done`) |

Runner status is cleared when a card transitions to `done` or `not_planned`.

## Kill Switch Semantics

| Action                       | Scope                | Behavior                                               |
| ---------------------------- | -------------------- | ------------------------------------------------------ |
| Stop (card)                  | Single card          | Kills specific container, sets `runner_status: killed` |
| Stop All                     | All cards in project | Kills all containers for the project                   |
| `runner.enabled: false`      | Global               | Disables all runner features (requires restart)        |
| Per-project `enabled: false` | Single project       | Hides run button for that project                     |

## Graceful Shutdown

The runner handles `SIGTERM` with a structured shutdown sequence to avoid
dropping in-flight work or leaving orphaned containers:

1. **`/readyz` flips to 503** — immediately upon receiving SIGTERM, `/readyz`
   begins returning `{"ok":false,"reason":"draining"}` so load balancers and
   Kubernetes stop routing new webhook traffic.
2. **In-flight requests finish** — any webhook requests already being processed
   (trigger, message, promote, etc.) are allowed to complete normally. No new
   requests are accepted.
3. **Tracked containers killed** — all containers currently tracked by the
   runner are sent a kill signal. Each killed container is reported as
   `runner_status: failed` to ContextMatrix so the card is not left in a
   phantom running state.
4. **Manager drain** — the container manager is given up to **30 seconds** to
   finish draining (completing any pending state transitions and final log
   flushes).
5. **Force-cleanup** — after the 30s drain window, a **5-second** hard timeout
   fires to release any remaining resources before the process exits.

## See Also

The runner-side implementation of the interactive protocol (HITL endpoints,
`CM_INTERACTIVE` entrypoint branching, stdin injection) is tracked in
**CTXRUN-026** in the contextmatrix-runner board.
