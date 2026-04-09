# CLAUDE.md — ContextMatrix

## What is this project?

ContextMatrix is a kanban-style task coordination system designed for AI agents
and humans. Cards are markdown files with YAML frontmatter, stored in a git
repository. It exposes a REST API and web UI for managing tasks across multiple
projects. The primary users are Claude Code agents that claim tasks, execute
them in separate project repos, and report progress back to the board.

ContextMatrix is a **coordination layer only**. It does not clone, build, or
interact with project code repositories. Each project's `.board.yaml` has a
`repo` field pointing to the code repo — agents use this to know where to work,
but ContextMatrix never touches it.

## Reference documents

Read these when working on the relevant area:

| Document                                               | Contents                                                                                                                                                    |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`docs/architecture.md`](docs/architecture.md)         | Component responsibilities, data flow, git repo scope, file layout. Read when modifying service layer, store, git, or lock interactions.                    |
| [`docs/agent-workflow.md`](docs/agent-workflow.md)     | Agent orchestration model, skill files, slash commands, workflow steps, blocker recovery. Read when working on MCP, skills, or agent coordination.          |
| [`docs/data-model.md`](docs/data-model.md)             | Domain rules (full detail), card file format, Go type definitions, board config format. Read when modifying card parsing, state machine, or API validation. |
| [`docs/api-reference.md`](docs/api-reference.md)       | REST endpoints, agent identification, error format, response codes. Read when modifying or consuming API handlers.                                          |
| [`docs/gotchas.md`](docs/gotchas.md)                   | YAML parsing, go-git, SSE, MCP, Vite, stdlib quirks. Skim before your first commit in a session.                                                            |
| [`docs/remote-execution.md`](docs/remote-execution.md) | Remote execution architecture, webhook protocol, container lifecycle, worker safety, operator endpoints, runner config reference, graceful shutdown. Read when working on runner integration or MCP auth.                  |
| [`web/CLAUDE.md`](web/CLAUDE.md)                       | Frontend conventions, Everforest color palette, UI semantic mappings. Auto-loaded when working in `web/`.                                                   |

## Architecture

```
cmd/contextmatrix/main.go    → entrypoint, wires dependencies, starts server
internal/board/              → domain types: Card, ProjectConfig, StateMachine
internal/storage/            → Store interface + FilesystemStore implementation
internal/gitops/             → GitManager (commit, pull, push via go-git)
internal/lock/               → agent claim/release/heartbeat + timeout checker
internal/service/            → CardService: orchestrates store, git, lock, events, state machine
internal/api/                → REST API handlers (stdlib http.ServeMux) + SSE endpoint
internal/mcp/                → MCP server: tools + prompts
internal/runner/             → webhook client for contextmatrix-runner
internal/jira/               → Jira client, epic importer, write-back handler
internal/events/             → in-process pub/sub event bus
internal/config/             → global config loading
internal/ctxlog/             → request_id context logger (WithRequestID / Logger)
internal/metrics/            → Prometheus metric vars + Register()
web/                         → React frontend (Vite + TypeScript + Tailwind)
skills/                      → Agent skill files (markdown, served via MCP prompts)
```

The boards data directory is a **separate git repository** (see
`docs/architecture.md`). It is NOT part of the source tree. See
`docs/architecture.md` for component responsibilities, data flow, and git repo
details.

## Tech stack

- **Go 1.26+** — backend
- **net/http** — stdlib HTTP router (Go 1.22+ supports method routing and path
  params)
- **go-git** — git operations (`github.com/go-git/go-git/v5`)
- **go-yaml v3** — YAML frontmatter (`gopkg.in/yaml.v3`)
- **goldmark** — markdown rendering for preview (`github.com/yuin/goldmark`)
- **Go MCP SDK** — MCP server via Streamable HTTP
  (`github.com/modelcontextprotocol/go-sdk`)
- **React 19 + TypeScript** — frontend
- **Vite** — frontend build
- **Tailwind CSS** — styling
- **@dnd-kit** — drag and drop (`@dnd-kit/core`, `@dnd-kit/sortable`)
- **@uiw/react-md-editor** — markdown editor
- **embed.FS** — frontend embedded into Go binary for single-binary distribution

## Coding conventions

### Go

- Use `internal/` for all packages — nothing exported outside the module.
- Interfaces belong in the package that _uses_ them, not the package that
  implements them. Exception: `storage.Store` is defined in `storage/` because
  multiple packages use it.
- Error handling: wrap with `fmt.Errorf("operation: %w", err)`. Never swallow
  errors.
- Use `context.Context` as the first parameter for any function that does I/O.
- No global state. Dependencies injected via struct fields, wired in `main.go`.
- Tests next to code: `card.go` → `card_test.go`. Use table-driven tests.
- Use `t.Helper()` in test helpers. Use `testify/assert` for assertions and
  `testify/require` for fatal checks (`github.com/stretchr/testify`).
- No `init()` functions.
- Logging: `log/slog` with structured fields. No `fmt.Println` in production.
- Names: `CardFilter` not `CardFilterStruct`. `ParseCard` not
  `ParseCardFromBytes`.
- Prefer returning concrete types from constructors, interfaces from consumers.
- All exported functions that write to disk or network must accept
  `context.Context`.

### Frontend

Frontend conventions, color palettes (Everforest and Radix), and UI semantic
mappings: see `web/CLAUDE.md` (auto-loaded when working in `web/`).

The `theme` config setting (`"everforest"` default, `"radix"` / `"catppuccin"`
alternatives) sets the server-side default palette, but users can override it
per-browser via the PaletteSelector in `AppHeader`. The chosen palette is
persisted in `localStorage` under the key `palette` and takes precedence over
the server default on subsequent loads. Components use CSS custom properties
only — no palette-specific code in components.

### Skills (`skills/`)

- Skills are agent instructions, not documentation. Keep them tight: clear
  instructions only, no explanatory commentary or rationale.
- Every sentence should be actionable — if it doesn't tell the agent what to do,
  remove it.

## Key domain rules (summary)

Full details with examples: `docs/data-model.md`.

1. **Card IDs:** `PREFIX-NNN`, zero-padded, server-generated, immutable.
2. **State transitions:** enforced per `.board.yaml` `transitions` map. 409 on
   invalid.
3. **One agent per card.** Claim required. Agent checked via `X-Agent-ID` header
   (403 on mismatch).
4. **Human identity:** agent IDs prefixed with `human:` (e.g., `human:alice`).
5. **Every mutation auto-commits** via `GitManager.CommitFile()`.
6. **Activity log:** append-only, capped at 50 entries per card.
7. **Heartbeat timeout:** default 30min, service layer sets card to `stalled` +
   clears agent. `stalled` is system-managed; `not_planned` is manual-only.
8. **`not_planned` state:** built-in like `stalled`, but follows normal
   transition rules — only states that explicitly list `not_planned` in their
   `.board.yaml` transitions can reach it (no server-side auto-injection). From
   `not_planned`, only `todo` is allowed. Releases agent claim, flushes deferred
   commits, excluded from active agent and open task counts.
9. **External source tracking:** `source` field for Jira/GitHub imports,
   immutable after creation.
10. **Parent auto-transitions:** parent goes `in_progress` when first subtask
    claimed. Stays in `in_progress` when all subtasks done — orchestrator
    transitions to `review` after documentation.
11. **Subtask type:** automatic when `parent` is set, immutable, built-in (not
    in `.board.yaml` types).
12. **Duplicate subtask guard:** `CreateCard` with a `parent` deduplicates by
    title (case-insensitive, trimmed). If an identically-titled subtask exists
    in a non-terminal state (`done`/`not_planned` excepted), the existing card
    is returned instead of creating a new one. Prevents re-entry after
    crash/restart from producing orphaned duplicate cards.
13. **Promote to autonomous:** `POST /api/projects/{project}/cards/{id}/promote`
    (human-only) flips `autonomous: true`, appends an activity log entry, and
    commits. Idempotent; rejects terminal cards with 409. The
    `promote_to_autonomous` MCP tool provides the same operation and is also
    human-only — `agent_id` must start with `"human:"` or the call is rejected.
    The runner's `/promote` webhook calls this endpoint first (fail-closed)
    before writing the canned stdin message — promotion without a successful API
    call is a no-op.

## Running the project

```bash
# Backend
make build        # builds binary with embedded frontend
make run          # runs on :8080
make test         # runs all Go tests

# Frontend dev (hot reload, proxies API to :8080)
cd web && npm run dev

# Install config and skills into your user config directory (see below)
make install-config

# Initialize the boards repo (separate from source code)
mkdir -p ~/boards/contextmatrix
cd ~/boards/contextmatrix && git init

# Create a new project board
mkdir -p project-alpha/tasks project-alpha/templates
# create project-alpha/.board.yaml (see format above)
# update config.yaml: boards.dir: ~/boards/contextmatrix

# Claude Code MCP config (~/claude.json or project .claude/claude.json)
# {
#   "mcpServers": {
#     "contextmatrix": {
#       "type": "http",
#       "url": "http://localhost:8080/mcp",
#       "headers": { "Authorization": "Bearer YOUR_MCP_API_KEY" }
#     }
#   }
# }
#
# For container deployments, point at the container:
# "url": "http://contextmatrix:8080/mcp"
```

## Install script and config template

`config.yaml.example` in the repo root is a fully-commented configuration
template. It documents every field, its default value, and the corresponding
`CONTEXTMATRIX_*` environment variable override.

`scripts/install.sh` copies the template and agent skill files into the user
config directory. The config directory is resolved via the XDG spec:

- `$XDG_CONFIG_HOME/contextmatrix` — if `XDG_CONFIG_HOME` is set
- `~/.config/contextmatrix` — otherwise

### Usage

```bash
# Fresh install: create config dir, copy config.yaml.example → config.yaml,
# copy skills/ directory. Skips config.yaml if it already exists.
make install-config
# or equivalently:
scripts/install.sh

# Only update the skills/ directory; config.yaml is not touched.
scripts/install.sh --update-skills

# Overwrite config.yaml even if it already exists (re-install).
scripts/install.sh --force
```

On a fresh install the script creates `~/.config/contextmatrix/config.yaml` from
the template. Edit `boards.dir` (and any other fields) before starting the
server. Skills are always refreshed from the repo's `skills/` directory even
without `--update-skills` — that flag simply skips the config.yaml step
entirely.

## Agent permissions in target projects

When ContextMatrix agents work on code in other repositories, those projects
must allow `Edit` and `Write` tools in their Claude Code permissions (e.g.,
`.claude/settings.local.json`). Without these, agents cannot modify files and
will report `TASK_BLOCKED`. See `docs/agent-workflow.md` for the full list of
required permissions.

## Mandatory verification before proceeding

**Every task must be fully tested and verified before moving to the next task.**

1. All unit tests pass: `go test ./internal/...` — zero failures.
2. Full suite passes: `make test` — no regressions.
3. Code compiles cleanly: `go build ./...` — zero errors.
4. Manual verification for API tasks: curl endpoints, confirm response codes.
   _(This is for human developers verifying API handler code. Agents must use
   MCP tools — see "Agent interaction rules" below.)_
5. Manual verification for frontend tasks: browser user flow end-to-end.
6. Manual verification for MCP tasks: connect Claude Code, invoke tools/prompts.
7. Lint passes: `golangci-lint run` clean.

If any check fails, fix before proceeding.

## Commit discipline

```bash
make test   # must be clean before every commit
make lint   # must be clean before every commit
make build  # must build
```

**NEVER** commit code without manual approval from the user. No exceptions.

**NEVER** reference the plan phase or task number in commit messages. Use
conventional commits:

**ALWAYS** keep the commit messages short, clear and focues. Use bullet points
in the message body to explain the "what" and "why" of the change, but avoid
long paragraphs.

**ALWAYS** write conventional commit messages with a type, scope, and concise
description. For example:

```
feat(mcp): Add MCP server with Streamable HTTP transport and tool definitions
feat(mcp): Add prompts capability for Claude Code slash commands
feat(skills): Add execute-task skill with heartbeat discipline
```

## Testing

- **Unit tests:** card parsing, state machine, lock manager, ID generation,
  event bus.
- **Integration tests:** service layer (real filesystem, temp dir), API
  (`httptest`), git commit verification.
- **Concurrent tests:** multiple goroutines claiming/updating simultaneously.
- **MCP tests:** tool call round-trips via in-process MCP client, prompt
  rendering.

Run tests frequently. Write tests alongside each function. Use `t.TempDir()`.

## Agent interaction rules

Agents interacting with ContextMatrix (via MCP) must follow these rules:

- **Always use MCP tools.** For all board interactions — claiming cards,
  heartbeats, updating cards, completing tasks, creating cards, transitioning
  state — ALWAYS use the provided MCP tools. Never use `curl`, `wget`, or direct
  HTTP API calls to interact with the board.
- The REST API exists for the web UI and for human developers to verify behavior
  during development. It is not the agent interface.
- If an MCP tool does not exist for an operation you need, report it as blocked
  rather than falling back to HTTP calls.
