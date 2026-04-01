# Architecture Details

## Data flow for a mutation (e.g., create card)

```
API handler (deserialize request, validate input)
  → CardService.CreateCard()
    → StateMachine.ValidateCard() — check type, state, priority are valid
    → Store.CreateCard() — write .md file to disk, update in-memory index
    → GitManager.CommitFile() — git add + commit
    → EventBus.Publish(CardCreated) — notify SSE subscribers
  ← return card to handler
← serialize response
```

The service layer is the single orchestration point. API handlers are thin —
they deserialize, call the service, serialize the response. No business logic in
handlers.

## Component responsibilities

Clear ownership to avoid confusion during implementation:

- **Store** (`storage.FilesystemStore`): reads/writes `.md` files and
  `.board.yaml` to disk. Maintains in-memory index. Has NO knowledge of git,
  events, or locking. Pure data access.
- **GitManager** (`gitops.Manager`): stages and commits files. Has NO knowledge
  of cards, stores, or events. Takes a file path and commit message, does git
  operations.
- **Lock Manager** (`lock.Manager`): enforces claim/release/heartbeat rules.
  Reads cards via the store to check ownership, but does NOT write to the store.
  Returns modified card data to the caller. The **caller** (Service Layer)
  handles the store write, git commit, and event publish.
- **Event Bus** (`events.Bus`): pure pub/sub. Receives events, fans out to
  subscribers. No logic.
- **StateMachine** (`board.StateMachine`): validates transitions and card
  fields. Pure functions, no side effects.
- **CardService** (`service.CardService`): the ONLY component that orchestrates
  multi-step operations. Every mutation follows: validate → store write → git
  commit → event publish. This includes claim/release AND the heartbeat timeout
  checker goroutine. The timeout checker lives in the service layer, not the
  lock manager, because it needs to coordinate store + git + events.
- **API handlers** (`api/*`): deserialize request → call CardService method →
  serialize response. No business logic, no direct store/git/lock access.
- **MCP server** (`mcp/*`): exposes `tools` (card operations) and `prompts`
  (slash commands / skill file injection) via Streamable HTTP on `POST /mcp`,
  registered on the same `http.ServeMux` as the REST API. Started and stopped
  with the main server. Calls CardService — same as API handlers, no business
  logic.

## Git repository scope

**The boards directory is a completely separate git repository from the
ContextMatrix source code.** They must never share the same git repo.

```
~/code/contextmatrix/       # source code git repo
  cmd/
  internal/
  web/
  skills/
  go.mod
  config.yaml               # boards_dir: ~/boards/contextmatrix

~/boards/contextmatrix/     # separate git repo for board data
  project-alpha/
    .board.yaml
    tasks/
    templates/
  project-beta/
    .board.yaml
    tasks/
    templates/
```

The `GitManager` is initialized at `cfg.BoardsDir` — the boards directory, NOT
the source code root. File paths passed to `CommitFile()` are relative to
`cfg.BoardsDir`, e.g., `project-alpha/tasks/ALPHA-001.md`.

The default `config.yaml` should NOT use `./boards` (which implies a
subdirectory of the source repo). Use an absolute path or a path outside the
source tree. Example:

```yaml
boards_dir: ~/boards/contextmatrix
```

If the boards directory doesn't exist or isn't a git repo on startup, the server
should create the directory and run `git init`.

## File layout on disk

**Source code repo** (e.g., `~/code/contextmatrix/`):

```
cmd/contextmatrix/main.go
internal/
web/
skills/
  create-task.md
  create-plan.md
  execute-task.md
  review-task.md
  document-task.md
  init-project.md
go.mod
config.yaml
Makefile
CLAUDE.md
```

**Boards repo** (e.g., `~/Development/contextmatrix-boards/` — separate git
repo):

```
project-alpha/
  .board.yaml
  templates/
    task.md
    bug.md
    feature.md
  tasks/
    ALPHA-001.md
    ALPHA-002.md
project-beta/
  .board.yaml
  templates/
  tasks/
```
