# Remote Execution

Remote execution allows autonomous tasks to be triggered from the ContextMatrix
web UI via a **"Run Now" button**. The workflow is executed by a separate binary
(**contextmatrix-runner**) that spawns disposable Docker containers with Claude
Code.

## Architecture Overview

```text
                                   HMAC-signed webhooks
                      ┌──────────────────────────────────────────────────┐
                      │                                                  ▼
  ┌──────────────┐    │    ┌───────────────────┐    ┌────────────────────────┐
  │  Web UI      │────┘    │  contextmatrix    │    │ contextmatrix-runner   │
  │  (Run Now)   │─────────│  (REST API)       │───►│  /trigger              │
  │  (Console)   │◄────────│  (SSE proxy)      │◄───│  /kill  /stop-all      │
  │  (Chat input)│─────────│  POST /message    │───►│  /message              │
  │  (Promote)   │─────────│  POST /promote    │───►│  /promote              │
  └──────────────┘         │  POST /mcp        │◄───│  Docker containers     │
                           │  (MCP tools)      │◄───│  (Claude Code)         │
                           └───────────────────┘    └────────────────────────┘
                                  ▲                            │
                                  │      MCP (Bearer auth)     │
                                  └────────────────────────────┘
```

**Message paths:**

- **Run Now / Stop / Stop All** — trigger/kill/stop-all webhooks from CM to runner.
- **Live log streaming** — runner exposes `GET /logs` SSE endpoint; CM proxies as
  `GET /api/runner/logs`. Web UI opens an `EventSource` only while the Runner
  Console panel is open.
- **Chat input** (interactive mode only) — Web UI sends
  `POST /api/runner/message` to CM, which forwards to the runner's `/message`
  endpoint. The runner writes the message to the container's stdin and echoes it
  as a `user` log entry.
- **Promote to autonomous** — Web UI sends `POST /api/runner/promote` to CM,
  which forwards to the runner's `/promote` endpoint. The runner emits a
  `system` log entry and injects a canned autonomous-mode prompt into stdin.

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
over the wire.

**Signing process:**

1. Marshal the JSON payload body
2. Compute `HMAC-SHA256(shared_secret, body)`
3. Hex-encode the result
4. Set header: `X-Signature-256: sha256=<hex>`

**Verification:** The receiver computes the expected HMAC and compares using
constant-time comparison.

### ContextMatrix → Runner Webhooks

All requests are `POST` with `Content-Type: application/json`.

#### POST {runner_url}/trigger

Sent when a user clicks "Run Now" on an autonomous card.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project",
  "repo_url": "git@github.com:org/repo.git",
  "mcp_url": "http://contextmatrix:8080/mcp",
  "mcp_api_key": "optional-bearer-token",
  "runner_image": "optional/custom-image:latest",
  "base_branch": "develop",
  "interactive": false
}
```

`base_branch` is omitted when not set on the card. When present, the runner
should clone using `-b <base_branch>` and instruct Claude Code to open PRs
against that branch instead of the repository default. See the runner card
CTXRUN-019 for the implementation on the runner side.

`interactive` defaults to `false`. When `true`, the runner sets the
`CM_INTERACTIVE=1` container environment variable and launches Claude Code with
`--input-format stream-json --output-format stream-json`. The container
entrypoint uses an initial prompt that instructs the agent to await the user's
first message before taking action. See [Interactive Mode](#interactive-mode)
for details.

**Note:** when `interactive: true`, `feature_branch` and `create_pr` are **not**
auto-enabled at run time. They are enabled later only if the user invokes
"Switch to Autonomous" (see [`POST {runner_url}/promote`](#post-runner_urlpromote)).

#### POST {runner_url}/kill

Sent when a user clicks "Stop" on a running card.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project"
}
```

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
   {"type":"user","message":{"role":"user","content":[{"type":"text","text":"<content>"}]}}
   ```
3. Emits a broadcaster `LogEntry` of type `user` (see [LogEntry types](#logentry-types))
   so the browser sees the message echoed in the console.

**Error responses:**

| Status | Condition |
|--------|-----------|
| 404 | Container not tracked (card not running) |
| 409 | Container is not in interactive mode |
| 413 | `content` exceeds 8 KiB |

#### POST {runner_url}/promote

Sent when a user clicks "Switch to Autonomous" while a container is running in
interactive mode. HMAC-signed identically to trigger/kill.

```json
{
  "card_id": "PROJ-042",
  "project": "my-project"
}
```

The runner:

1. Looks up the running container for `card_id` / `project`.
2. Emits a `system` `LogEntry` with content `"promoted to autonomous mode"`.
3. Writes a canned stream-json user message to the container's stdin instructing
   Claude Code to complete the workflow autonomously: create a feature branch,
   commit, push, and open a PR.

**Error responses:** same as `/message` (404, 409, 413 not applicable for
promote).

### Runner → ContextMatrix: SSE Log Stream

#### GET {runner_url}/logs

Streams live log entries via Server-Sent Events. Used by the ContextMatrix
proxy endpoint — not called directly by the browser.

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

| type | Source | Meaning |
|---|---|---|
| `text` | Claude Code stdout | Parsed assistant text block |
| `thinking` | Claude Code stdout | Parsed assistant thinking block |
| `tool_call` | Claude Code stdout | Non-MCP tool call (name only) |
| `stderr` | Container stderr | Raw stderr line from the container |
| `system` | Runner lifecycle | Container lifecycle events (started, completed, failed, canceled) |
| `user` | Chat input | User message submitted via the chat input |

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
  "error": "description of what went wrong"
}
```

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
   - MCP URL and API key injected as environment variables
   - Git credentials mounted (not baked into image)
   - The card ID and project name passed as arguments
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
  - Click "Run Now" (trigger remote execution)
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
authoritative for whether the "Run Now" button should be enabled.

### Global Kill Switch

Set `runner.enabled: false` in `config.yaml` to disable remote execution
entirely. The "Run Now" button will not appear in the UI, and trigger endpoints
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
  `--input-format stream-json --output-format stream-json`. The initial prompt
  instructs the agent to introduce itself, describe the card it has claimed, and
  then **await the user's first message** before taking any action.

### Message Flow

```
Web UI (chat input) → CM POST /api/runner/message
                     → Runner POST /message
                     → container stdin (stream-json user message)
                     → Runner LogEntry{type: "user"}  (echoed to browser)

Web UI (promote btn) → CM POST /api/runner/promote
                      → Runner POST /promote
                      → container stdin (canned autonomous-mode prompt)
                      → Runner LogEntry{type: "system", content: "promoted to autonomous mode"}
```

### Deferred Flags

When `interactive: true`, the `feature_branch` and `create_pr` flags are **not**
set on the container at launch. The runner only enables them when it receives a
`/promote` request, at which point the canned prompt instructs Claude Code to
create a feature branch, commit, push, and open a PR before completing the task.

## Log Streaming Architecture

The live log pipeline has three layers:

### Runner: `internal/logbroadcast`

`Broadcaster` is a thread-safe fan-out hub. It manages a set of subscribers,
each with a buffered channel (256 entries). `Publish(LogEntry)` is
non-blocking: if a subscriber's buffer is full the entry is dropped and a
warning logged (slow subscriber protection).

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

`webhook.Handler.handleLogs` subscribes to the broadcaster, then streams
entries as `data: {json}\n\n` SSE events. It sends `: keepalive\n\n` comments
every 15 seconds. The write deadline is cleared on the underlying connection to
allow long-lived connections past the server's `WriteTimeout`.

### ContextMatrix: `GET /api/runner/logs` SSE Proxy

`api.runnerHandlers.streamRunnerLogs` is a transparent SSE proxy:

1. Asserts the browser's `http.ResponseWriter` implements `http.Flusher`.
2. Clears the write deadline via `http.ResponseController`.
3. Sends SSE headers and flushes immediately (triggers browser `onopen`).
4. Issues an HMAC-signed `GET {runner_url}/logs?project=` using a dedicated
   `http.Client` with `Timeout: 0` (no per-request deadline).
5. Reads upstream body line-by-line with `bufio.Scanner` (1 MB buffer).
6. Forwards `data:` lines and `: keepalive` comments verbatim, flushing after
   each.
7. Checks `r.Context().Done()` between lines; returns immediately on browser
   disconnect, which causes the upstream `GET /logs` request to be canceled.

The endpoint is only registered when `runner != nil` (i.e., runner is enabled
in config). The browser is never exposed to the upstream HMAC credentials.

### Frontend: On-demand connection

The `EventSource` is opened only while the Runner Console panel is visible.
`useRunnerLogs({ enabled })` connects on `enabled=true` and disconnects on
`enabled=false` or component unmount. This satisfies the requirement that no
log traffic flows when the console is closed.

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
  public_url: "http://cm.lan:8080" # Public URL for MCP endpoint sent to runner containers
```

Environment variable overrides:

- `CONTEXTMATRIX_MCP_API_KEY`
- `CONTEXTMATRIX_RUNNER_ENABLED`
- `CONTEXTMATRIX_RUNNER_URL`
- `CONTEXTMATRIX_RUNNER_API_KEY`
- `CONTEXTMATRIX_RUNNER_PUBLIC_URL`

### Runner (`config.yaml` — reference for runner implementor)

```yaml
# ContextMatrix connection
contextmatrix_url: "http://contextmatrix:8080"
api_key: "shared-hmac-secret" # Must match CM's runner.api_key

# Container defaults
docker_base_image: "contextmatrix/runner:latest"
max_concurrent: 3 # Max simultaneous containers
container_timeout: "2h" # Force-kill after this duration


# Claude Code auth
# The runner must be installed on a machine with a browser
# for initial `claude login` OAuth flow. Auth tokens are
# mounted into containers at runtime.

# GitHub App credentials
github_app:
  app_id: 0
  installation_id: 0
  private_key_path: ""
  # For GHEC-DR or GHES: set to the enterprise API base URL.
  # Must match github.api_base_url (or its derived value) in ContextMatrix config.
  # api_base_url: "https://api.acme.ghe.com"  # Env: CMR_GITHUB_API_BASE_URL
```

When using GitHub Enterprise, both sides must target the same host: set
`github.host` (or `github.api_base_url`) in ContextMatrix and
`github_app.api_base_url` in the runner to the same enterprise API endpoint.
The runner entrypoint derives the git host automatically from the repo URL sent
in the trigger payload, so no additional git configuration is needed.

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
| Per-project `enabled: false` | Single project       | Hides Run Now for that project                         |

## See Also

The runner-side implementation of the interactive protocol (HITL endpoints,
`CM_INTERACTIVE` entrypoint branching, stdin injection) is tracked in
**CTXRUN-026** in the contextmatrix-runner board.
