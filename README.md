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
git_auto_push: false
heartbeat_timeout: "30m"
skills_dir: ./skills
token_costs:
  claude-sonnet-4: { prompt: 0.000003, completion: 0.000015 }
  claude-opus-4: { prompt: 0.000015, completion: 0.000075 }
EOF

# Run
./contextmatrix
```

Open `http://localhost:8080` for the web UI.

## Web UI

- **Board view** — drag-and-drop kanban columns per project, with card detail panel
- **Dashboard** — per-project state counts, active agents, and token cost breakdown
- **Swimlane view** — all projects in a single horizontal view (`/all`)
- **Theme toggle** — sun/moon icon in the header switches between Everforest dark and
  light palettes. The preference is persisted in `localStorage` and defaults to your
  system's `prefers-color-scheme` setting if no preference is stored.

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
states: [todo, in_progress, blocked, review, done, stalled]
types: [task, bug, feature]
priorities: [low, medium, high, critical]
transitions:
  todo: [in_progress]
  in_progress: [blocked, review, todo]
  blocked: [in_progress, todo]
  review: [done, in_progress]
  done: [todo]
  stalled: [todo, in_progress]
```

Optionally add templates in `templates/task.md`, `templates/bug.md`, etc. These
populate the card body when creating cards of that type.

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

When `mcp.auth_token` is set in `config.yaml`, the endpoint requires
`Authorization: Bearer <token>`.

### MCP Tools

| Tool                  | Description                                                 |
| --------------------- | ----------------------------------------------------------- |
| `list_projects`       | List all projects with configs                              |
| `create_project`      | Create a new project board                                  |
| `update_project`      | Update project configuration                                |
| `delete_project`      | Delete a project (must have zero cards)                     |
| `list_cards`          | List cards with filters (state, type, label, agent, parent) |
| `get_card`            | Get a single card                                           |
| `create_card`         | Create a card (returns generated ID)                        |
| `update_card`         | Update card fields                                          |
| `transition_card`     | Change card state (validated against state machine)         |
| `claim_card`          | Claim exclusive ownership of a card                         |
| `release_card`        | Release a claim                                             |
| `heartbeat`           | Update heartbeat timestamp (prevents stalling)              |
| `add_log`             | Append an activity log entry                                |
| `complete_task`       | Atomically log + transition to done + release               |
| `get_task_context`    | Get card + parent + siblings + project config in one call   |
| `get_ready_tasks`     | Get unclaimed todo cards with all dependencies met          |
| `get_subtask_summary` | Get subtask counts by state for a parent card               |
| `report_usage`        | Report token usage and estimated cost                       |

### Slash Commands

Skill files in `skills/` are served as MCP prompts, available as Claude Code
slash commands:

| Command                        | Argument      | Description                               |
| ------------------------------ | ------------- | ----------------------------------------- |
| `/contextmatrix:create-task`   | `description` | Guided task creation with human interview |
| `/contextmatrix:create-plan`   | `card_id`     | Break a task into executable subtasks     |
| `/contextmatrix:execute-task`  | `card_id`     | Claim and execute a task (for sub-agents) |
| `/contextmatrix:review-task`   | `card_id`     | Devils-advocate review of completed work  |
| `/contextmatrix:document-task` | `card_id`     | Write external documentation for a task   |
| `/contextmatrix:init-project`  | `name`        | Initialize a new project board            |

## Agent Workflow

Claude Code acts as the main orchestrator, spawning sub-agents via the `Task`
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
    "states": ["todo", "in_progress", "blocked", "review", "done", "stalled"],
    "types": ["task", "bug", "feature"],
    "priorities": ["low", "medium", "high", "critical"],
    "transitions": {
      "todo": ["in_progress"],
      "in_progress": ["blocked", "review", "todo"],
      "blocked": ["in_progress", "todo"],
      "review": ["done", "in_progress"],
      "done": ["todo"],
      "stalled": ["todo", "in_progress"]
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
  -d '{"agent_id": "claude-1", "model": "claude-sonnet-4", "prompt_tokens": 5000, "completion_tokens": 1200}'
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
`card.usage_reported`, `project.created`, `project.updated`, `project.deleted`.

### Health Check

```bash
curl http://localhost:8080/healthz
```

## Configuration

### config.yaml

| Field               | Default                 | Description                                    |
| ------------------- | ----------------------- | ---------------------------------------------- |
| `port`              | `8080`                  | HTTP server port                               |
| `boards_dir`        | ---                     | Path to boards git repo (required)             |
| `git_auto_commit`   | `true`                  | Auto-commit card mutations to git              |
| `git_auto_push`     | `false`                 | Auto-push after each commit                    |
| `heartbeat_timeout` | `"30m"`                 | Duration before a claimed card becomes stalled |
| `cors_origin`       | `http://localhost:5173` | Allowed CORS origin for the web UI             |
| `skills_dir`        | `./skills`              | Path to skill file markdown directory          |
| `token_costs`       | ---                     | Per-model token cost rates (see example below) |

Token cost configuration:

```yaml
token_costs:
  claude-sonnet-4: { prompt: 0.000003, completion: 0.000015 }
  claude-opus-4: { prompt: 0.000015, completion: 0.000075 }
```

### Environment Variables

All config fields can be overridden with environment variables:

- `CONTEXTMATRIX_PORT`
- `CONTEXTMATRIX_BOARDS_DIR`
- `CONTEXTMATRIX_GIT_AUTO_COMMIT`
- `CONTEXTMATRIX_GIT_AUTO_PUSH`
- `CONTEXTMATRIX_HEARTBEAT_TIMEOUT`
- `CONTEXTMATRIX_CORS_ORIGIN`
- `CONTEXTMATRIX_SKILLS_DIR`

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
