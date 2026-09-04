# AGENTS.md - ContextMatrix

## What is this project?

ContextMatrix is a kanban-style task coordination system for AI agents and
humans. Cards are markdown files with YAML frontmatter, stored in a git
repository. It exposes a REST API, an MCP server, and a web UI for managing
tasks across projects. Cards are executed by `contextmatrix-agent`, a
multi-model Go harness - driving OpenRouter or any OpenAI-compatible gateway -
that claims a card, works it in a separate project repo, and reports progress
back to the board over MCP. The global chat panel is served by
`contextmatrix-chat`, a sister backend that runs a worker container per chat
session. Humans drive the same board through the web UI.

**Boundary: ContextMatrix is a coordination layer only.** It never clones,
builds, or touches project code repositories. Each project's `.board.yaml`
carries a `repo` field pointing at the code repo; agents read it to know where
to work. The boards data directory is itself a **separate git repository** - not
part of this source tree (see `docs/architecture.md`).

## Reference documents

Read the relevant one before working in its area:

| Document                       | Read before working on                                                                                                                            |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/architecture.md`         | Component responsibilities, data flow, git-repo scope, and the **full trust model** (forks on `auth.mode`). Read before any security/auth review. |
| `docs/agent-workflow.md`       | MCP, skills, agent coordination. § Task skills covers the two-channel design.                                                                     |
| `docs/data-model.md`           | Card parsing, state machine, API validation - full domain rules and Go type definitions.                                                          |
| `docs/api-reference.md`        | REST endpoints, agent identification, error format, response codes.                                                                               |
| `docs/gotchas.md`              | YAML, go-git, SSE, MCP, Vite, stdlib quirks. Skim before your first commit each session.                                                          |
| `docs/remote-execution.md`     | Agent and chat backends, webhook protocol, worker lifecycle, MCP auth.                                                                            |
| `docs/model-selection.md`      | Model catalog pipeline, AA rating, tier bars, pins/favorites/blacklist, Best-of-N outcomes.                                                       |
| `docs/configuration.md`        | Config discovery, `CONTEXTMATRIX_*` override rules, CLI subcommands, state-dir stores, troubleshooting.                                           |
| `docs/authentication.md`       | `auth.mode` on the wire: bootstrap, invites, admin gate, credential pool, escape hatches, security posture.                                       |
| `docs/boards.md`               | Project creation, every `.board.yaml` field, body templates, the six built-in states, custom workflow skills.                                     |
| `docs/web-ui.md`               | Routes, board and sorting behaviour, dashboard, console, chat panes, images, appearance, shortcuts.                                               |
| `docs/playbooks.md`            | Entry kinds, storage and the reserved name, REST and MCP surface.                                                                                 |
| `docs/mcp.md`                  | Every MCP tool with its caller gate, slash commands, phase skills, payload rules.                                                                 |
| `docs/running-cards.md`        | Run Auto / Run HITL, fast path, guardrails, execution fields, Best-of-N, mob, PR gates, parking, stop.                                            |
| `docs/shared-boards.md`        | Shared-repo sync cycle, merge rules, per-instance claims and leases, several boards repos.                                                        |
| `docs/github-issue-import.md`  | Importer config, owner/repo resolution, vetting gate, `source` field, Enterprise hosts.                                                           |
| `web/AGENTS.md`                | Frontend. Auto-loaded when working in `web/`.                                                                                                     |

## Trust model (summary - canonical detail in `docs/architecture.md`)

Two auth postures, chosen by `auth.mode` (`CONTEXTMATRIX_AUTH_MODE` / config
`auth.mode`). **`multi` is the default** when unset. Confirm the live mode
before any auth review - the security properties fork on it.

- **`none`** - single-tenant, no logins; "loopback or behind a network ACL." If
  you can reach the API, you are trusted. `X-Agent-ID` is _identity for the
  audit trail, not authentication_ - like `git config user.name` on a personal
  machine.
- **`multi`** - login required for essentially the whole API (session guard in
  `internal/api/auth.go`); passwords are argon2id-hashed, session tokens stored
  only as SHA-256. The session-derived `human:<username>` **always wins** over
  any `X-Agent-ID` header, upgrading the card-ownership check into real
  enforcement. `requireAdmin` gates user, credential, project, admin-chat, and
  model-outcome/blacklist management only; card work needs just a session.

**Both modes:** MCP is Bearer-token auth (`mcp_api_key`); backend webhooks are
HMAC-signed; `/healthz` + `/readyz` are open; the admin listener (pprof +
`/metrics`) is loopback-only; the CSRF gate (`X-Requested-With: contextmatrix`)
runs unconditionally.

**Don't re-flag these as vulnerabilities** (rationale in
`docs/architecture.md`):

- `none` mode's `human:web` / `human:api` fallback when `X-Agent-ID` is absent
  on UI-only or human-only endpoints.
- The browser-generated agent ID being "spoofable" in `none` mode - there is no
  permission gradient to escalate into.
- MCP `create_project` / `update_project` / `delete_project` not sitting behind
  an admin gate - MCP has no role concept, and `update_project` structurally
  cannot touch credential bindings.

The `human:` prefix gate on human-only MCP tools (e.g. `promote_to_autonomous`)
enforces a workflow contract, not a security boundary, in both modes.

## Architecture

```
cmd/contextmatrix/main.go  → entrypoint; wires dependencies, starts server
internal/board/            → domain types: Card, ProjectConfig, StateMachine
internal/storage/          → Store interface + FilesystemStore
internal/gitops/           → GitManager (commit/pull/push via go-git, merges via shell git) + async CommitQueue
internal/gitsync/          → background board-repo sync
internal/boardmerge/       → pure three-way merge rules for a shared boards repo
internal/lock/             → claim/release/heartbeat + timeout checker
internal/service/          → CardService: orchestrates store, git, lock, events, state machine
internal/api/              → REST handlers (stdlib http.ServeMux) + SSE endpoint
internal/mcp/              → MCP server: tools + prompts
internal/backend/          → task-backend webhook client + reconcile sweep + end-session subscriber + session-log bridge (sessionlog subpkg)
internal/chat/             → chat session manager, SSE hub, chat-backend log bridge (persists via opstore)
internal/opstore/          → ops.db (SQLite): chat sessions/messages, model outcomes, blacklist, cost archive
internal/modelcatalog/     → cached model catalog + candidate rating (Artificial Analysis + OpenRouter or OpenAI-compatible endpoint)
internal/auth/             → sessions, users, one-time tokens, credential-pool crypto, master key
internal/authstore/        → auth.db (SQLite): users, sessions, tokens, credentials
internal/images/           → content-hashed image store: images.db plus <project>/images/ files in shared boards repos
internal/github/           → GitHub auth helpers shared across services
internal/clock/            → injectable clock for service-layer time invariants
internal/events/           → in-process pub/sub event bus
internal/config/           → global config loading
internal/ctxlog/           → request_id context logger
internal/metrics/          → Prometheus metric vars + Register()
web/                       → React frontend (Vite + TypeScript + Tailwind)
workflow-skills/           → agent lifecycle skills (markdown, served via MCP prompts)
```

## Tech stack

- **Go 1.26+** backend; **net/http** stdlib router (method + path-param
  routing).
- **go-git v5** (`github.com/go-git/go-git/v5`) - git operations.
- **gopkg.in/yaml.v3** - YAML frontmatter.
- **Go MCP SDK** (`github.com/modelcontextprotocol/go-sdk`) - MCP over
  Streamable HTTP.
- **modernc.org/sqlite** - pure-Go SQLite for `auth.db` and `ops.db` (no cgo).
- **`contextmatrix-githubauth`** - shared GitHub auth module (App + PAT +
  caching); imported by the server and the execution backends.
- **`contextmatrix-protocol`** - shared wire types across the server, the agent
  backend, and the chat backend.
- **React 19 + TypeScript**, **Vite**, **Tailwind**, **@dnd-kit**,
  **@uiw/react-md-editor**; frontend embedded into the Go binary via `embed.FS`
  for single-binary distribution.

## Coding conventions

### Go

- `internal/` for all packages - nothing exported outside the module.
- Interfaces live in the package that _uses_ them, not the one that implements
  them. Exception: `storage.Store` (multiple consumers).
- Wrap errors with `fmt.Errorf("operation: %w", err)`. Never swallow.
- `context.Context` is the first parameter of any function doing I/O; required
  for every exported function that writes to disk or network.
- No global state - inject dependencies via struct fields, wired in `main.go`.
  No `init()` functions.
- Tests next to code (`card.go` → `card_test.go`), table-driven, `t.TempDir()`,
  `t.Helper()` in helpers. `testify/assert` for checks, `testify/require` for
  fatal ones.
- Tests must exercise behavior. A test that only fails when someone rewords a
  string, renames a heading, or edits prose is not a test - do not write it, and
  delete it when found. Asserting exact text is legitimate only where the text is
  a wire contract a consumer parses; if no code reads it, no test should pin it.
- Logging: `log/slog` with structured fields. No `fmt.Println` in production.
- Names: `CardFilter` not `CardFilterStruct`; `ParseCard` not
  `ParseCardFromBytes`. Concrete types from constructors, interfaces from
  consumers.
- **GitHub auth goes exclusively through `githubauth.TokenGenerator`.** Never
  read raw tokens from config or env.

### Frontend

See `web/AGENTS.md` (auto-loaded in `web/`). Components reference CSS custom
properties only - no palette-specific code, no hardcoded hex.

### Skills - two systems, don't conflate

- **Workflow skills** (`workflow-skills/`) - lifecycle scaffolding served via
  MCP prompts; drive _what_ the agent does. Keep them tight: actionable
  instructions only, no commentary.
- **Task skills** - operator-provided `SKILL.md` files telling the agent _how_ to
  do implementation work well. CM ships none: point `task_skills.dir` (optionally
  git-backed) at your own repo. The agent and chat backends clone the
  `{git_remote_url, ref}` pointer CM derives from it. See
  `docs/agent-workflow.md` § Task skills.
- **Never unit-test skill prose.** Workflow skills and task skills are
  instructions, not code - no Go test may assert on their wording, headings, or
  markdown syntax. TDD does not apply to authoring them. The only permitted
  automated check is structural: that a skill file a builder references exists
  and parses. Behavior belongs in tests against `internal/mcp` with fixture
  skills, never against `workflow-skills/`. The same rule covers `docs/*.md` and
  any other prose the repo ships.

### Documentation

- Document the current state - what exists now and why, not how it got here.
- Comments explain only non-obvious decisions, constraints, safety invariants,
  or workarounds. Do not narrate what code does or record change history; git
  holds history. Keep comments to one or two tight lines unless a longer
  explanation is genuinely necessary. Rewrite or delete if you find comments
  that don't follow this rule.
- Never use em-dashes; use hyphens (-).
- Never reference plan phases, task numbers, or private card IDs in doc comments.

## Key domain rules

Full detail and examples: `docs/data-model.md`.

1. **Card IDs** - `PREFIX-NNN`, zero-padded, server-generated, immutable.
2. **State transitions** - enforced per `.board.yaml` `transitions`; invalid
   → 409.
3. **One agent per card** - claim required; `X-Agent-ID` must match
   `assigned_agent` (403 on mismatch). A card in a terminal state (`done` /
   `not_planned`) cannot be claimed (409), except by the agent already holding
   the claim.
4. **Human identity** - agent IDs prefixed `human:` (e.g. `human:alice`).
5. **Every mutation auto-commits** via `GitManager.CommitFile()`. With
   `git_deferred_commit`, a claimed card's commits batch until release or
   completion, entry into `review` or `not_planned`, a stall, or a terminal
   worker status.
6. **Activity log** - append-only, capped at 50 entries per card.
7. **Heartbeat timeout** - default 30 min; the service sets the card to
   `stalled` and clears the agent. `stalled` is system-managed.
8. **`not_planned`** - manual-only; built-in but follows normal transition rules
   (a state reaches it only by listing it in `.board.yaml`). From `not_planned`,
   only `todo`, and only as an explicit single transition: terminal states are
   absorbing for the path walker, so `complete_task` can never resurrect a
   cancelled card via `not_planned` → `todo` → `done`. Releases the claim,
   flushes deferred commits, excluded from active-agent and open-task counts.
9. **External source tracking** - `source` field (Jira/GitHub imports),
   immutable after creation.
10. **Parent auto-transitions** - parent goes `in_progress` when its first
    subtask is claimed; the orchestrator moves it to `review` after
    documentation.
11. **Subtask type** - automatic when `parent` is set; immutable; built-in (not
    in `.board.yaml` types).
12. **Duplicate subtask guard** - `CreateCard` with a `parent` dedupes by title
    (case-insensitive, trimmed): an identically-titled subtask in a non-terminal
    state is returned instead of created. Prevents crash/restart re-entry from
    orphaning duplicates.
13. **Promote to autonomous** -
    `POST /api/projects/{project}/cards/{id}/promote` (human-only) flips
    `autonomous: true`, logs, and commits. Idempotent; rejects terminal cards
    with 409. The `promote_to_autonomous` MCP tool is the same operation, also
    human-only (`agent_id` must start with `human:`). The agent backend's
    `/promote` webhook calls this endpoint first, fail-closed.
14. **`assignee`** - human-only informational responsibility label (a bare
    username), fully independent of `assigned_agent` (the execution claim).
    Never a permission boundary. Validated against the user roster in multi
    mode; hidden entirely in none mode (there is no user roster to validate
    against).
15. **Playbooks** - cross-project ordered lists at `playbooks/<id>.yaml` in
    the boards repo; global/shared; entries are card refs (live status,
    never stored) or manual gate steps; entry notes are a human-only
    channel; `playbooks` is a reserved project name.

## Running & verifying

```bash
make build              # binary with embedded frontend (needs make install-frontend first)
make run                # runs on :8080
make test               # all Go tests; stubs web/dist for the embed (run first in a fresh clone)
make lint               # golangci-lint run (read-only, never rewrites the tree)
make install-frontend   # npm install in web/; web/node_modules is gitignored and absent in fresh clones
make test-frontend      # vitest run in web/
make lint-frontend      # eslint in web/
make test-integration   # real-binary harness, stub LLM, requires Docker
cd web && npm run dev   # frontend hot reload, proxies /api → :8080
make install-config     # copy config.yaml.example + workflow skills into your XDG config dir
```

Run `make test` **before any bare `go build ./...` or `go test ./...` in a fresh
clone** - it stubs `web/dist` for the `//go:embed all:dist` in `web/embed.go`,
which otherwise fails with `pattern all:dist: no matching files found`. Don't
hand-create the stub; `make test` owns it.

`web/node_modules` is gitignored, so a fresh clone or a container has none.
`make build`, `make test-frontend` and `make lint-frontend` all shell into `web/`
and fail until `make install-frontend` has populated `web/node_modules`. Run it
before the first build in any new checkout.

`config.yaml.example` is a fully-commented template documenting every field, its
default, and its `CONTEXTMATRIX_*` env override. `scripts/install.sh` copies it
(and the workflow skills) into `$XDG_CONFIG_HOME/contextmatrix` (or
`~/.config/contextmatrix`).

**Before moving to the next task, verify:**

- `make test` - clean, no regressions.
- `make lint` - clean.
- `make build` - builds (run `make install-frontend` first in a fresh checkout).
- When `web/` changed: `make test-frontend` and `make lint-frontend` - both clean.
- API / frontend / MCP changes - exercise the real surface (curl an endpoint,
  drive the browser flow, invoke the tool). Agents verify via MCP tools, never
  curl (see below).

Target-project agents need `Edit` and `Write` allowed in their Claude Code
permissions, or they report `TASK_BLOCKED` (see `docs/agent-workflow.md`).

## Commit discipline

Run before every commit:

```bash
make test      # clean
make lint      # clean
make build     # builds (run make install-frontend first in a fresh checkout)
```

- When `web/` changed: `make test-frontend` and `make lint-frontend` - both clean.
- **Never commit without explicit user approval.** No exceptions.
- Conventional commits: `type(scope): concise summary`. Always include a scope.
- Body uses bullet points for the what and why - no long paragraphs.
- Never reference plan phases, task numbers, or private card IDs in messages.

## Agent interaction rules

Agents interact with the board **only through MCP tools** - claim, heartbeat,
update, complete, create, transition. Never `curl`, `wget`, or direct HTTP. The
REST API exists for the web UI and for human developers verifying behavior. If
no MCP tool covers an operation, report it blocked rather than falling back to
HTTP.
