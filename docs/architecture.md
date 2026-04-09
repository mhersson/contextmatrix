# Architecture

## Data flow

Every card mutation follows the same pipeline through the service layer:

```text
API handler (deserialize, validate)
  → CardService
    → StateMachine.ValidateCard() — type, state, priority checks
    → Store.CreateCard()          — write .md file, update in-memory index
    → GitManager.CommitFiles()    — git add + commit
    → EventBus.Publish()          — notify SSE subscribers
  ← return card
← serialize response
```

The MCP server follows the same path — it calls `CardService` methods, never the
store or git layer directly.

## Component responsibilities

- **Store** (`storage.FilesystemStore`): reads/writes `.md` files and
  `.board.yaml` to disk. Maintains an in-memory index. No knowledge of git,
  events, or locking.
- **GitManager** (`gitops.Manager`): stages and commits files. Takes a file path
  and commit message. No knowledge of cards or events.
- **Lock Manager** (`lock.Manager`): enforces claim/release/heartbeat rules.
  Reads cards via the store to check ownership but does not write — it returns
  modified card data to the caller (the service layer).
- **Event Bus** (`events.Bus`): in-process pub/sub. Receives events, fans out to
  subscribers.
- **StateMachine** (`board.StateMachine`): validates transitions and card
  fields. Pure functions, no side effects.
- **CardService** (`service.CardService`): the only component that orchestrates
  multi-step operations. Every mutation follows: validate → store write → git
  commit → event publish. Also runs the heartbeat timeout checker goroutine,
  which lives here (not in the lock manager) because it coordinates store, git,
  and events.
- **API handlers** (`api/*`): thin HTTP layer. Deserialize → call CardService →
  serialize. No business logic, no direct store/git/lock access. The runner log
  proxy (`GET /api/runner/logs`) is the exception: it issues an outbound
  HMAC-signed SSE request to the runner and forwards the stream verbatim,
  closing the upstream connection when the browser disconnects.
- **MCP server** (`mcp/*`): exposes tools (card operations) and prompts (skill
  files) via Streamable HTTP on `POST /mcp`. Registered on the same
  `http.ServeMux` as the REST API.
- **Jira integration** (`jira/*`): HTTP client for the Jira REST API, epic
  importer (creates CM projects from Jira epics with child issues as cards),
  and a write-back handler that posts comments on Jira issues when cards
  complete. The write-back handler subscribes to the event bus and runs
  asynchronously — failures are logged but never block card state transitions.
  Auth auto-detects Cloud (Basic Auth) vs Server/DC (Bearer token) based on
  whether `email` is configured.

## Git repository scope

The boards directory is a separate git repository from the source code. The
`GitManager` operates on `cfg.BoardsDir`, not the source tree. File paths passed
to `CommitFiles()` are relative to that directory (e.g.,
`project-alpha/tasks/ALPHA-001.md`).

```text
~/code/contextmatrix/           # source code repo
  cmd/, internal/, web/, skills/
  config.yaml                   # boards_dir: ~/boards/contextmatrix

~/boards/contextmatrix/         # boards repo (separate git repo)
  project-alpha/
    .board.yaml
    tasks/
    templates/
```

If the boards directory does not exist or is not a git repo on startup, the
server creates it and runs `git init`.

`boards_dir` in `config.yaml` should point outside the source tree — an absolute
path or a path like `~/boards/contextmatrix`, not `./boards`.

## File layout

**Source code:**

```text
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
  run-autonomous.md
go.mod
config.yaml
Makefile
```

**Boards repo:**

```text
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
