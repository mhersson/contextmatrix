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
| [`docs/architecture.md`](docs/architecture.md)         | Component responsibilities, data flow, git repo scope, file layout, **trust model** (single-tenant, no auth — read before any security/auth review). |
| [`docs/agent-workflow.md`](docs/agent-workflow.md)     | Agent orchestration model, skill files, slash commands, workflow steps, blocker recovery. Read when working on MCP, skills, or agent coordination. § Task skills: two-channel design, guard/permit, description convention. |
| [`docs/data-model.md`](docs/data-model.md)             | Domain rules (full detail), card file format, Go type definitions, board config format. Read when modifying card parsing, state machine, or API validation. |
| [`docs/api-reference.md`](docs/api-reference.md)       | REST endpoints, agent identification, error format, response codes. Read when modifying or consuming API handlers.                                          |
| [`docs/gotchas.md`](docs/gotchas.md)                   | YAML parsing, go-git, SSE, MCP, Vite, stdlib quirks. Skim before your first commit in a session.                                                            |
| [`docs/remote-execution.md`](docs/remote-execution.md) | Remote execution architecture, webhook protocol, container lifecycle, worker safety, operator endpoints, runner config reference, graceful shutdown. Read when working on runner or agent task backend integration, or MCP auth.  |
| [`docs/agent-backend-parity.md`](docs/agent-backend-parity.md) | Agent backend v1 parity matrix, intentional divergences, and the enable recipe. Read when selecting or validating the agent task backend. |
| [`web/CLAUDE.md`](web/CLAUDE.md)                       | Frontend conventions, Everforest color palette, UI semantic mappings. Auto-loaded when working in `web/`.                                                   |

## Trust model (read this before any auth/identity review)

ContextMatrix is a **single-tenant, no-auth** coordination tool. There are no
user accounts, no logins, no per-user permissions. The deployment story is
"loopback or behind a network ACL" (see `docs/api-reference.md` on the admin
listener — same posture). If you can reach the API, you are trusted.

**`X-Agent-ID` is identity, not authentication.** It tags writes for the
audit trail (boards-repo commit author, activity log entries, `assigned_agent`
on cards). It cannot prove the caller is who they say they are because there
is no auth layer underneath. Treat it the way you treat `git config user.name`
on a personal machine — the user could lie, but they have no incentive to.

**UI = human, by design.** The web UI is operated by a human; the CSRF gate
(`X-Requested-With: contextmatrix`) is the UI-origin signal, not an auth
check. The frontend auto-generates a per-browser identity
(`human:web-<8 hex chars>`, persisted in localStorage by `useAgentId`) so
two browsers on the same instance have distinct claim identities. We do
**not** prompt users for usernames — that's pointless theatre on a tool
with no auth, and the user has rejected that pattern explicitly.

**Don't re-flag these as security issues:**
- The `human:web` REST fallback when `X-Agent-ID` is absent on write endpoints
  where the UI is the only legitimate caller.
- The `human:api` fallback for runner human-only endpoints
  (`internal/api/runner.go`). Same reasoning.
- The lack of auth on read endpoints, project CRUD, sync, branches, app
  config, healthz/readyz, etc.
- The browser-generated agent ID being "spoofable." Spoofing it accomplishes
  nothing — there is no permission gradient to escalate into.

**Where identity gates DO matter:**
- MCP tools that gate on `human:` prefix (e.g., `promote_to_autonomous`).
  Reason: MCP is the agent interface, so an agent caller would lack the
  prefix; the gate enforces a workflow contract ("only humans promote"), not a
  security boundary. The prefix check is intentionally weak (any
  `human:anything` passes).
- Card-claim / heartbeat / release endpoints check that the supplied
  `X-Agent-ID` matches `assigned_agent`. This prevents two agents from
  stepping on each other, not unauthorized access.
- GitHub authentication via the shared `githubauth` module — that's real
  auth against an external system; do not weaken or bypass.

When in doubt: **"UI = human, case closed."**

## Architecture

```
cmd/contextmatrix/main.go    → entrypoint, wires dependencies, starts server
internal/board/              → domain types: Card, ProjectConfig, StateMachine
internal/storage/            → Store interface + FilesystemStore implementation
internal/gitops/             → GitManager (commit, pull, push via go-git) + async CommitQueue
internal/gitsync/            → background board-repo sync
internal/lock/               → agent claim/release/heartbeat + timeout checker
internal/service/            → CardService: orchestrates store, git, lock, events, state machine
internal/api/                → REST API handlers (stdlib http.ServeMux) + SSE endpoint
internal/mcp/                → MCP server: tools + prompts
internal/runner/             → webhook client for task backends (contextmatrix-runner and contextmatrix-agent)
internal/chat/               → SQLite-backed chat session manager, SSE hub, runner-log bridge
internal/images/             → content-hashed image blob store (paste/drop screenshots)
internal/clock/              → injectable clock for service-layer time invariants
internal/events/             → in-process pub/sub event bus
internal/github/             → GitHub auth helpers shared across services
internal/config/             → global config loading
internal/ctxlog/             → request_id context logger (WithRequestID / Logger)
internal/metrics/            → Prometheus metric vars + Register()
web/                         → React frontend (Vite + TypeScript + Tailwind)
workflow-skills/             → Agent skill files (markdown, served via MCP prompts)
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
- **`github.com/mhersson/contextmatrix-githubauth`** — shared GitHub
  auth module (App + PAT + caching). Imported by both the server and
  the runner.
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
- GitHub authentication is handled exclusively via
  `githubauth.TokenGenerator` from the shared module. Do not introduce
  new code paths that read raw tokens from config or env vars; the
  provider abstraction is the only entry point.

### Frontend

Frontend conventions, color palettes (Everforest and Radix), and UI semantic
mappings: see `web/CLAUDE.md` (auto-loaded when working in `web/`).

The `theme` config setting (`"everforest"` default, `"radix"` / `"catppuccin"`
alternatives) sets the server-side default palette, but users can override it
per-browser via the PaletteSelector in `AppHeader`. The chosen palette is
persisted in `localStorage` under the key `palette` and takes precedence over
the server default on subsequent loads. Components use CSS custom properties
only — no palette-specific code in components.

### Skills (`workflow-skills/`)

- Skills are agent instructions, not documentation. Keep them tight: clear
  instructions only, no explanatory commentary or rationale.
- Every sentence should be actionable — if it doesn't tell the agent what to do,
  remove it.

**Two skill systems — don't conflate them:**

- **Workflow skills** (`workflow-skills/`) — lifecycle scaffolding served via
  MCP prompts. Drive what the agent does.
- **Task skills** (`task-skills/`, `task_skills.dir`) — curated Claude Code
  skills (`SKILL.md`) that tell the agent how to do implementation work well.
  Both the runner and the agent backend are consumers: the runner bind-mounts
  the resolved subset into the worker container; the agent fetches a
  `{git_remote_url, ref}` pointer from `GET /api/agent/task-skills-source` and
  mounts it read-only. Both report first engagement via `RecordSkillEngaged`
  (runner: `POST /api/runner/skill-engaged`; agent: MCP `add_log
  action=skill_engaged`). See `docs/agent-workflow.md` § Task skills for full
  details.

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
make build                  # builds binary with embedded frontend
make run                    # runs on :8080
make test                   # runs all Go tests
make test-integration       # real-binary harness (stub worker, ~70s, requires Docker)

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
# copy workflow skills into <config-dir>/workflow-skills/. Skips config.yaml
# if it already exists.
make install-config
# or equivalently:
scripts/install.sh

# Only update the workflow-skills/ directory; config.yaml is not touched.
scripts/install.sh --update-workflow-skills

# Add-only refresh of task-skills/ — never overwrites user edits.
scripts/install.sh --update-task-skills

# Overwrite config.yaml even if it already exists (re-install).
scripts/install.sh --force
```

On a fresh install the script creates `~/.config/contextmatrix/config.yaml` from
the template. Edit `boards.dir` (and any other fields) before starting the
server. Workflow skills are always refreshed from the repo's
`workflow-skills/` directory even without `--update-workflow-skills` — that
flag simply skips the config.yaml step entirely.

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
