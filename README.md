# ContextMatrix

Kanban-style task coordination for AI agents and humans. Cards are markdown
files with YAML frontmatter, stored in a git repository. Every mutation is
auto-committed, giving you a full audit trail.

ContextMatrix is a coordination layer — it tracks tasks but never touches your
project code repositories. AI agents (Claude Code, Gemini, etc.) claim tasks,
execute them in their own repos, and report progress back through the board.

## Quick Start

```bash
# Build (requires Go 1.26+ and Node.js 18+)
make install-frontend
make build

# Configure boards directory (a separate git repo for task data)
mkdir -p ~/boards/contextmatrix
cd ~/boards/contextmatrix && git init

# Create config.yaml
cat > config.yaml <<EOF
port: 8080
boards_dir: ~/boards/contextmatrix
git_auto_commit: true
git_deferred_commit: false
git_auto_push: false
git_auto_pull: false
git_pull_interval: "60s"
heartbeat_timeout: "30m"
skills_dir: ./skills
token_costs:
  claude-haiku-4-5:  { prompt: 0.0000008, completion: 0.000004  }
  claude-sonnet-4-6: { prompt: 0.000003,  completion: 0.000015  }
  claude-opus-4-6:   { prompt: 0.000005,  completion: 0.000025  }
EOF

# Run
./contextmatrix
```

Open `http://localhost:8080` for the web UI.

## Web UI

- **Board view** — drag-and-drop kanban columns per project, with card detail
  panel. Columns can be collapsed to a narrow vertical strip by clicking the
  left-arrow button in the column header. Individual cards can be collapsed to a
  single header row (ID, type badge, and truncated title) using the chevron
  button on each card. Both collapsed column and collapsed card sets are
  persisted per-project in `localStorage`.
- **Dashboard** — per-project or all state counts, active agents, and token cost
  breakdown
- **Theme toggle** — sun/moon icon in the header switches between Everforest
  dark and light palettes. The preference is persisted in `localStorage` and
  defaults to your system's `prefers-color-scheme` setting if no preference is
  stored.

## Creating a Board

Each project lives in a subdirectory of the boards repo with a `.board.yaml`.
You can create projects via the API (`POST /api/projects`) or manually:

```bash
mkdir -p ~/boards/contextmatrix/my-project/tasks
mkdir -p ~/boards/contextmatrix/my-project/templates
```

```yaml
# ~/boards/contextmatrix/my-project/.board.yaml
name: my-project
prefix: MYPROJ
next_id: 1
repo: git@github.com:org/my-project.git
states: [todo, in_progress, blocked, review, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high, critical]
transitions:
  todo: [in_progress, not_planned]
  in_progress: [blocked, review, todo]
  blocked: [in_progress, todo]
  review: [done, in_progress]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
```

Optionally add templates in `templates/task.md`, `templates/bug.md`, etc. These
populate the card body when creating cards of that type.

## Installation

The install script copies the configuration template and agent skill files into
your user config directory.

```bash
# Fresh install: create config dir, copy config.yaml from template, copy skills/
make install-config
# or equivalently:
scripts/install.sh

# Only update the skills/ directory — config.yaml is not touched
scripts/install.sh --update-skills

# Overwrite config.yaml even if it already exists (re-install)
scripts/install.sh --force
```

**Config directory** is resolved via the XDG Base Directory spec:

- `$XDG_CONFIG_HOME/contextmatrix` — if `XDG_CONFIG_HOME` is set
- `~/.config/contextmatrix` — otherwise

**What gets installed:**

- `config.yaml` — copied from `config.yaml.example` (skipped if it already
  exists, unless `--force`)
- `skills/` — the agent skill files from the repo's `skills/` directory (always
  refreshed)

After a fresh install, edit `boards_dir` in
`~/.config/contextmatrix/config.yaml` before starting the server. The
`skills_dir` defaults to the `skills/` directory next to the config file, so no
manual path update is needed.

## MCP Integration

ContextMatrix exposes an MCP server on `POST /mcp` (Streamable HTTP transport).
Connect Claude Code by adding this to your MCP config (`~/.claude/claude.json`
or project `.claude/claude.json`):

```json
{
  "mcpServers": {
    "contextmatrix": {
      "type": "http",
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

### MCP Tools

| Tool                        | Description                                                 |
| --------------------------- | ----------------------------------------------------------- |
| `add_log`                   | Append an activity log entry                                |
| `check_agent_health`        | Check health of subtask agents for a parent card            |
| `claim_card`                | Claim exclusive ownership of a card                         |
| `complete_task`             | Atomically log + transition to done + release               |
| `create_card`               | Create a card (returns generated ID)                        |
| `create_project`            | Create a new project board                                  |
| `delete_project`            | Delete a project (must have zero cards)                     |
| `get_card`                  | Get a single card                                           |
| `get_ready_tasks`           | Get unclaimed todo cards with all dependencies met          |
| `get_skill`                 | Get a skill prompt with injected card/project context       |
| `get_subtask_summary`       | Get subtask counts by state for a parent card               |
| `get_task_context`          | Get card + parent + siblings + project config in one call   |
| `heartbeat`                 | Update heartbeat timestamp (prevents stalling)              |
| `increment_review_attempts` | Increment the review attempt counter on a card              |
| `list_cards`                | List cards with filters (state, type, label, agent, parent) |
| `list_projects`             | List all projects with configs                              |
| `recalculate_costs`         | Recalculate token costs for cards with missing cost data    |
| `release_card`              | Release a claim                                             |
| `report_push`               | Report a git push for a card                                |
| `report_usage`              | Report token usage and estimated cost                       |
| `transition_card`           | Change card state (validated against state machine)         |
| `update_card`               | Update card fields                                          |
| `update_project`            | Update project configuration                                |

### Slash Commands

Skill files in `skills/` are served as MCP prompts, available as Claude Code
slash commands:

| Command                         | Argument      | Description                               |
| ------------------------------- | ------------- | ----------------------------------------- |
| `/contextmatrix:create-task`    | `description` | Guided task creation with human interview |
| `/contextmatrix:create-plan`    | `card_id`     | Break a task into executable subtasks     |
| `/contextmatrix:execute-task`   | `card_id`     | Claim and execute a task (for sub-agents) |
| `/contextmatrix:review-task`    | `card_id`     | Devils-advocate review of completed work  |
| `/contextmatrix:document-task`  | `card_id`     | Write external documentation for a task   |
| `/contextmatrix:init-project`   | `name`        | Initialize a new project board            |
| `/contextmatrix:run-autonomous` | `card_id`     | Run full autonomous lifecycle for a card  |

## Agent Workflow

Claude Code acts as the main orchestrator, spawning sub-agents via the `Agent`
tool. The typical workflow:

1. **Create** — `/contextmatrix:create-task` interviews the human and creates a
   card
2. **Plan** — `/contextmatrix:create-plan` breaks it into subtasks with
   dependencies
3. **Execute** — `/contextmatrix:execute-task` runs in parallel sub-agents,
   each:
   - Calls `claim_card` for exclusive ownership
   - Works the task, calling `heartbeat` after each significant step (30min
     timeout)
   - Calls `complete_task` when done
4. **Review** — `/contextmatrix:review-task` provides a devils-advocate
   assessment
5. **Document** — `/contextmatrix:document-task` writes docs after review
   approval

Cards with `depends_on` relationships are enforced — a card cannot transition to
`in_progress` until all its dependencies are `done`. The `get_ready_tasks` tool
returns only cards eligible for execution.

## States, Transitions, and Skills

The state machine is fully customizable per project via `.board.yaml`. You can
define any states, any allowed transitions, and any card types to match your
team's workflow.

However, the built-in skill files installed by `make install-config` /
`scripts/install.sh` are written against the **default** states and transitions:

| Default state | Role                                              |
| ------------- | ------------------------------------------------- |
| `todo`        | Ready to be claimed                               |
| `in_progress` | Actively being worked                             |
| `blocked`     | Waiting on an external dependency                 |
| `review`      | Work complete, awaiting review                    |
| `done`        | Accepted and finished                             |
| `stalled`     | Heartbeat timed out; claim released automatically |
| `not_planned` | Deprioritized; excluded from active counts        |

Skill dependencies on specific states:

- **`execute-task`** — expects `in_progress`, `blocked`, and `review` states. It
  transitions the card to `in_progress` on claim and to `review` on completion.
- **`review-task`** — requires a `review` state to transition into and out of.
  Without it the skill cannot function.
- **`create-plan`** and **`document-task`** — rely on `done` as the terminal
  state.

If you remove or rename states (e.g. drop `review`), the default skills will
break. In that case:

1. Copy the `skills/` directory to a custom location.
2. Edit the relevant skill files to match your state names.
3. Set `skills_dir` in `config.yaml` to point to your custom directory.

The default skills are always refreshed from the repo by `scripts/install.sh`;
your custom directory is never touched by the install script.

## Autonomous Mode

Cards with `autonomous: true` run through the full lifecycle without human
approval gates. The `/contextmatrix:run-autonomous` slash command drives the
entire workflow for a single card:

```
plan → subtask creation → execute (parallel) → review → document → done
```

The orchestrator agent handles each phase in sequence, spawning sub-agents via
the `Agent` tool for execution, review, and documentation.

### Guardrails

- **Branch protection** — agents operating in autonomous mode must never push to
  `main` or `master`. The `report_push` MCP tool enforces this and returns a
  hard error if the branch name is `main` or `master`.
- **Maximum review cycles** — after 2 review cycles without passing review, the
  workflow halts and requires human intervention. The
  `increment_review_attempts` tool tracks the counter; the orchestrator checks
  it before spawning another review sub-agent.
- **Heartbeat-based stall detection** — if a sub-agent's heartbeat times out,
  the service layer marks the card `stalled` and releases the claim. The
  orchestrator uses `check_agent_health` to detect stalled sub-agents and
  respawn them automatically.

## Remote Execution

Remote execution lets you trigger autonomous tasks from the web UI. A **"Run
Now"** button appears on autonomous cards in `todo` state. Clicking it sends a
signed webhook to the **contextmatrix-runner** (a separate binary), which spawns
a disposable Docker container running Claude Code in headless mode. The
container connects back to ContextMatrix via MCP tools.

```
Web UI  →  ContextMatrix  →  contextmatrix-runner  →  Docker container
(Run Now)   (webhook)         (spawn container)        (Claude Code + MCP)
```

### Setup

```yaml
# config.yaml
runner:
  enabled: true
  url: "http://localhost:9090" # runner base URL
  api_key: "your-secret-key-min-32ch" # shared HMAC secret
  public_url: "http://contextmatrix:8080" # URL reachable from containers
mcp_api_key: "your-mcp-bearer-token" # MCP auth for container connections
```

Per-project, you can override the enabled flag and set a custom runner image in
`.board.yaml`:

```yaml
remote_execution:
  enabled: true
  runner_image: "ghcr.io/org/custom-runner:latest"
```

Cards track execution state via `runner_status`: `queued` → `running` →
`failed`/`killed`. The web UI shows status badges and pulsing indicators for
active tasks. See [`docs/remote-execution.md`](docs/remote-execution.md) for the
full architecture, webhook protocol, and security model.

## API

All endpoints are under `/api`. Agent identity is sent via the `X-Agent-ID`
header. Claimed cards can only be mutated by the owning agent.

### Projects

```bash
# List projects
curl http://localhost:8080/api/projects

# Create a project
curl -X POST http://localhost:8080/api/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-project",
    "prefix": "MYPROJ",
    "repo": "git@github.com:org/my-project.git",
    "states": ["todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"],
    "types": ["task", "bug", "feature"],
    "priorities": ["low", "medium", "high", "critical"],
    "transitions": {
      "todo": ["in_progress", "not_planned"],
      "in_progress": ["blocked", "review", "todo"],
      "blocked": ["in_progress", "todo"],
      "review": ["done", "in_progress"],
      "done": ["todo"],
      "stalled": ["todo", "in_progress"],
      "not_planned": ["todo"]
    }
  }'

# Get project config
curl http://localhost:8080/api/projects/my-project

# Update project config
curl -X PUT http://localhost:8080/api/projects/my-project \
  -H "Content-Type: application/json" \
  -d '{ "types": ["task", "bug", "feature", "epic"] }'

# Delete project (must have zero cards)
curl -X DELETE http://localhost:8080/api/projects/my-project

# Project dashboard (state counts, active agents, costs)
curl http://localhost:8080/api/projects/my-project/dashboard

# Aggregated token usage
curl http://localhost:8080/api/projects/my-project/usage
```

### Cards

```bash
# Create a card
curl -X POST http://localhost:8080/api/projects/my-project/cards \
  -H "Content-Type: application/json" \
  -d '{"title": "Implement auth", "type": "task", "priority": "high"}'

# List cards (with optional filters)
curl "http://localhost:8080/api/projects/my-project/cards?state=todo&type=task"

# Get a card
curl http://localhost:8080/api/projects/my-project/cards/MYPROJ-001

# Update a card (full)
curl -X PUT http://localhost:8080/api/projects/my-project/cards/MYPROJ-001 \
  -H "Content-Type: application/json" \
  -H "X-Agent-ID: claude-1" \
  -d '{"title": "Implement auth", "type": "task", "state": "in_progress", "priority": "high"}'

# Patch a card (partial)
curl -X PATCH http://localhost:8080/api/projects/my-project/cards/MYPROJ-001 \
  -H "Content-Type: application/json" \
  -H "X-Agent-ID: claude-1" \
  -d '{"state": "done"}'

# Delete a card
curl -X DELETE http://localhost:8080/api/projects/my-project/cards/MYPROJ-001 \
  -H "X-Agent-ID: claude-1"
```

### Agent Operations

```bash
# Claim a card
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/claim \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "claude-1"}'

# Send heartbeat
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "claude-1"}'

# Add activity log entry
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/log \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "claude-1", "action": "status_update", "message": "JWT middleware done"}'

# Release a card
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/release \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "claude-1"}'

# Get card context (card + parent + siblings + project config)
curl http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/context

# Report token usage
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/usage \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "claude-1", "model": "claude-sonnet-4-6", "prompt_tokens": 5000, "completion_tokens": 1200}'
```

### Server-Sent Events

```bash
# Stream all events
curl -N http://localhost:8080/api/events

# Stream events for a specific project
curl -N "http://localhost:8080/api/events?project=my-project"
```

Events: `card.created`, `card.updated`, `card.deleted`, `card.state_changed`,
`card.claimed`, `card.released`, `card.stalled`, `card.log_added`,
`card.usage_reported`, `project.created`, `project.updated`, `project.deleted`,
`runner.triggered`, `runner.started`, `runner.failed`, `runner.killed`.

### Remote Execution

These endpoints are human-only (agents with `X-Agent-ID` headers are rejected).
Requires `runner.enabled: true` in config.

```bash
# Trigger remote execution for an autonomous card in todo state
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/run

# Stop a running/queued task
curl -X POST http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/stop

# Stop all running tasks in a project
curl -X POST http://localhost:8080/api/projects/my-project/stop-all
```

The runner status callback (`POST /api/runner/status`) is HMAC-signed and used
by the contextmatrix-runner to report container state changes. See
[`docs/remote-execution.md`](docs/remote-execution.md) for the full webhook
protocol.

### Health Check

```bash
curl http://localhost:8080/healthz
```

## Configuration

### config.yaml

| Field                 | Default                 | Description                                                            |
| --------------------- | ----------------------- | ---------------------------------------------------------------------- |
| `port`                | `8080`                  | HTTP server port                                                       |
| `boards_dir`          | ---                     | Path to boards git repo (required)                                     |
| `git_auto_commit`     | `true`                  | Auto-commit card mutations to git                                      |
| `git_deferred_commit` | `false`                 | Batch commits until a terminal state (done/not_planned) is reached     |
| `git_auto_push`       | `false`                 | Auto-push after each commit                                            |
| `git_auto_pull`       | `false`                 | Pull from remote on startup and at `git_pull_interval`                 |
| `git_pull_interval`   | `"60s"`                 | How often to pull when `git_auto_pull` is enabled (Go duration string) |
| `heartbeat_timeout`   | `"30m"`                 | Duration before a claimed card becomes stalled                         |
| `cors_origin`         | `http://localhost:5173` | Allowed CORS origin for the web UI                                     |
| `skills_dir`          | `./skills`              | Path to skill file markdown directory                                  |
| `token_costs`         | ---                     | Per-model token cost rates (see example below)                         |
| `mcp_api_key`         | `""`                    | Bearer token for MCP endpoint authentication (empty = no auth)         |
| `runner.enabled`      | `false`                 | Enable remote execution integration                                    |
| `runner.url`          | `""`                    | Base URL of the contextmatrix-runner (e.g. `http://localhost:9090`)    |
| `runner.api_key`      | `""`                    | Shared secret for HMAC-SHA256 webhook signing (min 32 chars)           |
| `runner.public_url`   | `""`                    | Public URL of this instance, reachable from runner containers          |

Token cost configuration:

```yaml
token_costs:
  claude-haiku-4-5: { prompt: 0.0000008, completion: 0.000004 }
  claude-sonnet-4-6: { prompt: 0.000003, completion: 0.000015 }
  claude-opus-4-6: { prompt: 0.000005, completion: 0.000025 }
```

### Environment Variables

All config fields can be overridden with environment variables:

- `CONTEXTMATRIX_PORT`
- `CONTEXTMATRIX_BOARDS_DIR`
- `CONTEXTMATRIX_GIT_AUTO_COMMIT`
- `CONTEXTMATRIX_GIT_DEFERRED_COMMIT`
- `CONTEXTMATRIX_GIT_AUTO_PUSH`
- `CONTEXTMATRIX_GIT_AUTO_PULL`
- `CONTEXTMATRIX_GIT_PULL_INTERVAL`
- `CONTEXTMATRIX_HEARTBEAT_TIMEOUT`
- `CONTEXTMATRIX_CORS_ORIGIN`
- `CONTEXTMATRIX_SKILLS_DIR`
- `CONTEXTMATRIX_MCP_API_KEY`
- `CONTEXTMATRIX_RUNNER_ENABLED`
- `CONTEXTMATRIX_RUNNER_URL`
- `CONTEXTMATRIX_RUNNER_API_KEY`
- `CONTEXTMATRIX_RUNNER_PUBLIC_URL`

## Development

```bash
# Prerequisites: Go 1.26+, Node.js 18+, npm

# Run Go tests
make test

# Run linter
make lint

# Frontend dev server (hot reload, proxies API to :8080)
cd web && npm install && npm run dev

# Build binary with embedded frontend
make build
```

## License

MIT
