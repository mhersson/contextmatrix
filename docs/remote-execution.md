# Remote Execution

Remote execution allows autonomous tasks to be triggered from the ContextMatrix
web UI via a **"Run Now" button**. The workflow is executed by a separate binary
(**contextmatrix-runner**) that spawns disposable Docker containers with Claude
Code.

## Architecture Overview

```
                                   HMAC-signed webhooks
                      ┌──────────────────────────────────────────┐
                      │                                          ▼
  ┌──────────────┐    │    ┌───────────────────┐    ┌────────────────────┐
  │  Web UI      │────┘    │  contextmatrix    │    │ contextmatrix-     │
  │  (Run Now)   │─────────│  (REST API)       │───►│ runner             │
  └──────────────┘         │                   │    │                    │
                           │  POST /mcp        │◄───│  Docker containers │
                           │  (MCP tools)      │    │  (Claude Code)     │
                           └───────────────────┘    └────────────────────┘
                                  ▲                         │
                                  │    MCP (Bearer auth)    │
                                  └─────────────────────────┘
```

**ContextMatrix** is the coordination layer. It stores cards, manages state, and
sends webhooks to the runner. It never touches code repositories.

**contextmatrix-runner** is a separate binary (separate repository) that:
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
  "runner_image": "optional/custom-image:latest"
}
```

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

Valid `runner_status` values: `"running"`, `"failed"`.

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
   - Plans, executes, reviews, documents
   - Creates feature branch and PR
   - Completes the card via MCP `complete_task`
5. Container exits after workflow completes
6. Runner cleans up the container

**On kill:** Container is destroyed immediately. All uncommitted work is
discarded. No partial saves.

## Security Model

### Webhook Signing (HMAC)

A single shared secret (`runner.api_key` / `api_key`) authenticates both
directions:
- ContextMatrix signs outbound webhooks to the runner
- Runner signs status callbacks to ContextMatrix
- Uses HMAC-SHA256 — the secret never travels over the wire

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
- Agents inside containers cannot escalate themselves to autonomous mode

### Per-Project Kill Switch

Each project can override the global runner setting:

```yaml
# In .board.yaml
remote_execution:
  enabled: true                    # or false to disable for this project
  runner_image: "custom/image:v2"  # optional per-project Docker image
```

Resolution order:
1. Project's `remote_execution.enabled` (if set)
2. Global `runner.enabled` (fallback)

### Global Kill Switch

Set `runner.enabled: false` in `config.yaml` to disable remote execution
entirely. The "Run Now" button will not appear in the UI and trigger endpoints
return 503.

## Configuration Reference

### ContextMatrix (`config.yaml`)

```yaml
# MCP endpoint authentication (optional)
mcp_api_key: "your-bearer-token"

# Runner integration
runner:
  enabled: false
  url: "http://localhost:9090"     # Runner base URL
  api_key: "shared-hmac-secret"    # HMAC signing key
```

Environment variable overrides:
- `CONTEXTMATRIX_MCP_API_KEY`
- `CONTEXTMATRIX_RUNNER_ENABLED`
- `CONTEXTMATRIX_RUNNER_URL`
- `CONTEXTMATRIX_RUNNER_API_KEY`

### Runner (`config.yaml` — reference for runner implementor)

```yaml
# ContextMatrix connection
contextmatrix_url: "http://contextmatrix:8080"
api_key: "shared-hmac-secret"     # Must match CM's runner.api_key

# Container defaults
docker_base_image: "contextmatrix/runner:latest"
max_concurrent: 3                 # Max simultaneous containers
container_timeout: "2h"           # Force-kill after this duration

# Claude Code auth
# The runner must be installed on a machine with a browser
# for initial `claude login` OAuth flow. Auth tokens are
# mounted into containers at runtime.
```

### Per-Project (`.board.yaml`)

```yaml
remote_execution:
  enabled: true
  runner_image: "my-org/go-runner:latest"
```

## Card Runner Status

The `runner_status` field tracks the container lifecycle independently of the
card's workflow state:

| runner_status | Meaning |
|---|---|
| (empty) | Not associated with runner |
| `queued` | Trigger webhook sent, container not yet started |
| `running` | Container is running, CC is working |
| `failed` | Container crashed or webhook failed |
| `killed` | User stopped the task |

Runner status is cleared when a card transitions to `done` or `not_planned`.

## Kill Switch Semantics

| Action | Scope | Behavior |
|---|---|---|
| Stop (card) | Single card | Kills specific container, sets `runner_status: killed` |
| Stop All | All cards in project | Kills all containers for the project |
| `runner.enabled: false` | Global | Disables all runner features (requires restart) |
| Per-project `enabled: false` | Single project | Hides Run Now for that project |
