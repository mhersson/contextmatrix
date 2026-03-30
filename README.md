# ContextMatrix

Kanban-style task coordination for AI agents and humans. Cards are markdown
files with YAML frontmatter, stored in a git repository. Every mutation is
auto-committed, giving you a full audit trail.

ContextMatrix is a coordination layer — it tracks tasks but never touches your
project code repositories.

## Quick Start

```bash
# Build (requires Go 1.22+ and Node.js 18+)
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
EOF

# Run
./contextmatrix
```

Open `http://localhost:8080` for the web UI.

## Creating a Board

Each project lives in a subdirectory of the boards repo with a `.board.yaml`:

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

## API

All endpoints are under `/api`. Agent identity is sent via the `X-Agent-ID`
header. Claimed cards can only be mutated by the owning agent.

### Projects

```bash
# List projects
curl http://localhost:8080/api/projects

# Get project config
curl http://localhost:8080/api/projects/my-project
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

# Get card context (card + project config + template)
curl http://localhost:8080/api/projects/my-project/cards/MYPROJ-001/context
```

### Server-Sent Events

```bash
# Stream all events
curl -N http://localhost:8080/api/events

# Stream events for a specific project
curl -N "http://localhost:8080/api/events?project=my-project"
```

Events are published for: `card.created`, `card.updated`, `card.deleted`,
`card.state_changed`, `card.claimed`, `card.released`, `card.stalled`,
`card.log_added`.

## Configuration

### config.yaml

| Field               | Default | Description                                    |
| ------------------- | ------- | ---------------------------------------------- |
| `port`              | `8080`  | HTTP server port                               |
| `boards_dir`        | —       | Path to boards git repo (required)             |
| `git_auto_commit`   | `true`  | Auto-commit card mutations to git              |
| `git_auto_push`     | `false` | Auto-push after each commit                    |
| `heartbeat_timeout` | `"30m"` | Duration before a claimed card becomes stalled |

### Environment Variables

All config fields can be overridden with environment variables:

- `CONTEXTMATRIX_PORT`
- `CONTEXTMATRIX_BOARDS_DIR`
- `CONTEXTMATRIX_GIT_AUTO_COMMIT`
- `CONTEXTMATRIX_GIT_AUTO_PUSH`
- `CONTEXTMATRIX_HEARTBEAT_TIMEOUT`

## Development

```bash
# Prerequisites: Go 1.22+, Node.js 18+, npm

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
