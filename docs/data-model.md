# Data Model & Domain Rules

## Key domain rules

1. **Card IDs** are globally unique: `PREFIX-NNN`, zero-padded to 3 digits
   minimum. `ALPHA-001`, `ALPHA-042`, `ALPHA-999`, `ALPHA-1000` (grows past 3
   digits when needed). The server generates IDs by incrementing `next_id` in
   `.board.yaml`. IDs are immutable once created.

2. **State transitions are enforced.** Transitions defined in `.board.yaml`
   under `transitions`. API returns 409 Conflict with descriptive error on
   invalid transition. `stalled` is a system-managed state — the lock manager
   can transition any state → `stalled` (on heartbeat timeout), but
   agents/humans can only transition `stalled` → states listed in
   `transitions.stalled`.

3. **One agent per card.** `POST /claim` fails with 409 if card is already
   claimed. Only the assigned agent can mutate a claimed card — API checks
   `X-Agent-ID` header against `assigned_agent` and returns 403 on mismatch.
   Unclaimed cards can be mutated by anyone.

4. **Human identity.** Humans use agent IDs prefixed with `human:` (e.g.,
   `human:alice`). The claim system treats them identically to AI agents. The
   web UI stores the human's agent ID in localStorage and sends it via
   `X-Agent-ID` header.

5. **Every mutation auto-commits.** The service layer writes the file, then
   calls `GitManager.CommitFile()`. Commit message format:
   `[contextmatrix] CARD-ID: description` or
   `[agent:AGENT-ID] CARD-ID: description`.

6. **Activity log is append-only, capped at 50 entries.** Agents add entries via
   `POST /cards/{id}/log`. Older entries beyond 50 are dropped from the card
   file but preserved in git history. Entries are never edited or deleted.

7. **Heartbeat timeout.** If `last_heartbeat` exceeds configured timeout
   (default 30min), the service layer's background goroutine (not the lock
   manager) sets card state to `stalled`, clears `assigned_agent`, commits to
   git, and publishes a `CardStalled` event. The lock manager only identifies
   stalled cards; the service layer handles the full state change.

8. **External source tracking.** Cards imported from external systems (Jira,
   GitHub Issues, etc.) use the `source` field to record origin. The
   `source.external_id` field is indexed and queryable via
   `GET /cards?external_id=PROJ-1234`. This provides idempotent imports — check
   if the external ID exists before creating, update if it does. The `source`
   field is immutable after creation.

9. **Parent card auto-transitions on child state changes.** When a subtask is
   claimed (transitions to `in_progress`), the service layer automatically
   transitions the parent from `todo` → `in_progress` if it is currently in
   `todo`. When all subtasks reach `done`, the parent is automatically
   transitioned to `review`. These transitions are handled by the service
   layer's `maybeTransitionParent` helper after any `UpdateCard` or `PatchCard`
   call that causes a child's state to change. The `complete_task` MCP tool
   detects when the parent reaches `review` and returns embedded `review-task`
   skill content in the response, so the calling agent can spawn the review
   sub-agent immediately.

10. **Subtask type is automatic and immutable.** The service layer enforces
    subtask type invariants on both `CreateCard` and `UpdateCard` based on
    parent field transitions:

    | Scenario | Behaviour |
    |---|---|
    | Card is created with a non-empty `parent` | `type` is auto-forced to `"subtask"` regardless of caller input |
    | `UpdateCard` sets `parent` on a card that had none | `type` is auto-forced to `"subtask"` regardless of caller input |
    | `UpdateCard` clears `parent` on a card that had one | if `type` is still `"subtask"`, it is auto-reset to the first type in the project's `types` list (e.g. `"task"`) |
    | `UpdateCard` keeps an existing `parent` | `type` must remain `"subtask"`; any other value returns 422 |
    | Card has no `parent` (before or after) | `type: "subtask"` is rejected with 422 |

    The `subtask` type is built-in — it is always valid and does not need to
    appear in the project's `types` list in `.board.yaml`. A card's type is
    fully managed by the service layer whenever the `parent` field changes; do
    not pass `type` when setting or clearing `parent`.

## Card file format

```yaml
---
id: ALPHA-001
title: Implement user auth
project: project-alpha
type: task
state: in_progress
priority: high
assigned_agent: claude-7a3f
last_heartbeat: 2026-03-30T14:30:00Z
parent: ""
subtasks: [ALPHA-003, ALPHA-004]
depends_on: []
context:
  - src/auth/
  - docs/auth-spec.md
labels: [backend, security]
source:
  system: jira
  external_id: PROJ-1234
  external_url: https://company.atlassian.net/browse/PROJ-1234
custom:
  branch_name: feat/user-auth
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T14:30:00Z
activity_log:
  - agent: claude-7a3f
    ts: 2026-03-30T14:30:00Z
    action: status_update
    message: "JWT middleware done"
---
## Plan
...
## Progress
...
## Notes
...
```

A subtask card looks identical except `type` is always `subtask` and `parent` is
set. The server enforces this automatically (see domain rule 10):

```yaml
---
id: ALPHA-003
title: Implement token refresh
project: project-alpha
type: subtask
state: todo
priority: high
parent: ALPHA-001
depends_on: []
labels: [backend]
created: 2026-03-30T11:00:00Z
updated: 2026-03-30T11:00:00Z
---
```

The frontmatter is delimited by `---` lines. The body is freeform markdown. When
parsing, split on `---` — first element is empty (before opening delimiter),
second is YAML, third is body.

## Go type definitions

These are the authoritative struct definitions.

```go
// internal/board/card.go

type Card struct {
    ID            string            `yaml:"id"              json:"id"`
    Title         string            `yaml:"title"           json:"title"`
    Project       string            `yaml:"project"         json:"project"`
    Type          string            `yaml:"type"            json:"type"`
    State         string            `yaml:"state"           json:"state"`
    Priority      string            `yaml:"priority"        json:"priority"`
    AssignedAgent string            `yaml:"assigned_agent,omitempty"  json:"assigned_agent,omitempty"`
    LastHeartbeat *time.Time        `yaml:"last_heartbeat,omitempty" json:"last_heartbeat,omitempty"`
    Parent        string            `yaml:"parent,omitempty"         json:"parent,omitempty"`
    Subtasks      []string          `yaml:"subtasks,omitempty"       json:"subtasks,omitempty"`
    DependsOn     []string          `yaml:"depends_on,omitempty"     json:"depends_on,omitempty"`
    Context       []string          `yaml:"context,omitempty"        json:"context,omitempty"`
    Labels        []string          `yaml:"labels,omitempty"         json:"labels,omitempty"`
    Source        *Source           `yaml:"source,omitempty"         json:"source,omitempty"`
    Custom        map[string]any    `yaml:"custom,omitempty"         json:"custom,omitempty"`
    Created       time.Time         `yaml:"created"         json:"created"`
    Updated       time.Time         `yaml:"updated"         json:"updated"`
    ActivityLog   []ActivityEntry   `yaml:"activity_log,omitempty"   json:"activity_log,omitempty"`
    Body          string            `yaml:"-"               json:"body"`
}

type ActivityEntry struct {
    Agent     string    `yaml:"agent"   json:"agent"`
    Timestamp time.Time `yaml:"ts"      json:"ts"`
    Action    string    `yaml:"action"  json:"action"`
    Message   string    `yaml:"message" json:"message"`
}

type Source struct {
    System      string `yaml:"system"       json:"system"`
    ExternalID  string `yaml:"external_id"  json:"external_id"`
    ExternalURL string `yaml:"external_url" json:"external_url"`
}
```

```go
// internal/board/project.go

type ProjectConfig struct {
    Name        string              `yaml:"name"`
    Prefix      string              `yaml:"prefix"`
    NextID      int                 `yaml:"next_id"`
    Repo        string              `yaml:"repo,omitempty"`
    States      []string            `yaml:"states"`
    Types       []string            `yaml:"types"`
    Priorities  []string            `yaml:"priorities"`
    Transitions map[string][]string `yaml:"transitions"`
    Templates   map[string]string   `yaml:"-"` // loaded from templates/ dir at runtime
}
```

**Immutable fields** (set on creation, never changed): `id`, `project`,
`created`, `source`.

**Server-managed fields** (set by service layer, not by clients directly): `id`,
`created`, `updated`, `assigned_agent`, `last_heartbeat`, `activity_log`.

## Project board config format

```yaml
# boards/project-alpha/.board.yaml
name: project-alpha
prefix: ALPHA
next_id: 1
repo: git@github.com:org/project-alpha.git
states: [todo, in_progress, blocked, review, done, stalled]
types: [task, bug, feature] # "subtask" is built-in — do not add it here
priorities: [low, medium, high, critical]
transitions:
  todo: [in_progress]
  in_progress: [blocked, review, todo]
  blocked: [in_progress, todo]
  review: [done, in_progress]
  done: [todo]
  stalled: [todo, in_progress]
```

The `stalled` state must always be present in `states` and `transitions`. The
server enforces this.
