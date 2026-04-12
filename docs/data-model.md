# Data Model & Domain Rules

## Key domain rules

1. **Card IDs** are globally unique: `PREFIX-NNN`, zero-padded to 3 digits
   minimum. `ALPHA-001`, `ALPHA-042`, `ALPHA-999`, `ALPHA-1000` (grows past 3
   digits when needed). The server generates IDs by incrementing `next_id` in
   `.board.yaml`. IDs are immutable once created.

2. **State transitions are enforced.** Transitions defined in `.board.yaml`
   under `transitions`. API returns 409 Conflict with descriptive error on
   invalid transition. One state has a special built-in rule:
   - **`stalled`** is system-managed — the lock manager can transition any state
     → `stalled` (on heartbeat timeout), but agents/humans can only transition
     `stalled` → states listed in `transitions.stalled`. Stalled cards release
     the active agent claim.

   Both `stalled` and `not_planned` are required built-in states (the server
   validates their presence in every project config). Unlike `stalled`,
   `not_planned` follows normal transition rules — only states that explicitly
   list `not_planned` in their transitions can reach it. It is a terminal state:
   transitioning to `not_planned` releases the agent claim, flushes deferred
   commits, and excludes the card from active agent and open task counts. No
   automatic mechanism ever transitions a card to `not_planned`.

3. **One agent per card.** `POST /claim` fails with 409 if card is already
   claimed. Only the assigned agent can mutate a claimed card — API checks
   `X-Agent-ID` header against `assigned_agent` and returns 403 on mismatch.
   Unclaimed cards can be mutated by anyone.

4. **Human identity.** Humans use agent IDs prefixed with `human:` (e.g.,
   `human:alice`). The claim system treats them identically to AI agents. The
   web UI stores the human's agent ID in localStorage and sends it via
   `X-Agent-ID` header.

5. **Every mutation auto-commits (with optional deferral).** The service layer
   writes the file, then commits via `GitManager`. Commit message format:
   `[contextmatrix] CARD-ID: description` or
   `[agent:AGENT-ID] CARD-ID: description`. When `boards.git_deferred_commit: true` in
   `config.yaml`, agent mutations during a work session are batched and flushed
   as a single commit at claim release/completion. Card creation and human edits
   to unclaimed cards are always committed immediately regardless of this
   setting.

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
   field is immutable after creation. `source.external_url`, when present, must
   use an `http` or `https` scheme — any other scheme (e.g. `javascript:`,
   `data:`, `vbscript:`) is rejected at write time with a 422 validation error
   (`ErrInvalidExternalURL`).

9. **Human vetting gate for externally-imported cards.** Cards with a `source`
   field (external origin) carry a `vetted` boolean that defaults to `false` on
   creation. AI agents cannot claim an unvetted card — `ClaimCard` returns 403
   `CARD_NOT_VETTED` if `card.Source != nil && !card.Vetted`. A human must
   inspect the card content and toggle `vetted: true` via the web UI before any
   agent can work on it.

   - **Internal cards** (no `source` field) always have `vetted: true`; the
     guard does not apply and the field is irrelevant for them.
   - **`vetted` is a human-only field.** Agents receive 403 `HUMAN_ONLY_FIELD`
     if they attempt to set it via the API or MCP. The MCP `update_card` tool
     does not expose `vetted` at all — agents cannot self-vet cards.
   - The `get_ready_tasks` MCP tool automatically excludes unvetted external
     cards from its results so agents never see them as claimable work.
   - The web UI shows an "unvetted" badge on board cards and a warning banner
     in the card panel for cards with `source && !vetted`.

10. **Parent card auto-transitions on child state changes.** When a subtask is
   claimed (transitions to `in_progress`), the service layer automatically
   transitions the parent from `todo` → `in_progress` if it is currently in
   `todo`. When all subtasks reach `done`, the parent stays in `in_progress`
   — the orchestrator spawns a documentation sub-agent first, then manually
   transitions the parent to `review`. The `complete_task` MCP tool detects when all siblings
   are done and returns an informational message so the calling agent knows
   documentation can proceed.

11. **Subtask type is automatic and immutable.** The service layer enforces
    subtask type invariants on both `CreateCard` and `UpdateCard` based on
    parent field transitions:

    | Scenario                                            | Behaviour                                                                                                        |
    | --------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
    | Card is created with a non-empty `parent`           | `type` is auto-forced to `"subtask"` regardless of caller input                                                  |
    | `UpdateCard` sets `parent` on a card that had none  | `type` is auto-forced to `"subtask"` regardless of caller input                                                  |
    | `UpdateCard` clears `parent` on a card that had one | if `type` is still `"subtask"`, it is auto-reset to the first type in the project's `types` list (e.g. `"task"`) |
    | `UpdateCard` keeps an existing `parent`             | `type` must remain `"subtask"`; any other value returns 422                                                      |
    | Card has no `parent` (before or after)              | `type: "subtask"` is rejected with 422                                                                           |

    The `subtask` type is built-in — it is always valid and does not need to
    appear in the project's `types` list in `.board.yaml`. A card's type is
    fully managed by the service layer whenever the `parent` field changes; do
    not pass `type` when setting or clearing `parent`.

12. **Duplicate subtask guard.** When `CreateCard` is called with a `parent`
    set, the service layer checks for an existing subtask under that parent
    whose title matches (case-insensitive, whitespace-trimmed) and is in a
    **non-terminal** state (anything other than `done` or `not_planned`). If a
    match is found, the existing card is returned as-is — no new card is
    created. The response is identical in shape to a normal create response
    (200 with the card body), so callers do not need to handle this case
    specially.

    Rationale: LLM agents may re-enter Phase 2 (subtask creation) after a
    crash or context reset, causing the same subtask to be created twice with
    sequential IDs. The guard prevents orphaned duplicate cards without
    requiring callers to check first.

    - The check is under `writeMu`, so there is no TOCTOU race.
    - The `next_id` counter is still incremented and the gap is harmless.
    - If an identically-titled subtask exists but is already `done` or
      `not_planned`, a new card **is** created — duplicates of completed work
      are intentional (e.g., re-doing a failed step).

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
vetted: true
custom:
  some_key: some_value
autonomous: true
use_opus_orchestrator: false
feature_branch: true
create_pr: true
branch_name: feat/ALPHA-001-implement-user-auth
base_branch: develop
pr_url: https://github.com/org/repo/pull/42
review_attempts: 0
runner_status: ""
token_usage:
  model: claude-sonnet-4-6
  prompt_tokens: 12340
  completion_tokens: 5670
  estimated_cost_usd: 0.122
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

The `parent` field is displayed in the web UI wherever a subtask card appears:

- **Board (CardItem):** a clickable monospace badge showing the parent card ID
  appears in both the expanded footer and the collapsed header row. Clicking
  navigates to the parent card.
- **Detail panel (CardPanelMetadata):** a "Parent" section appears above
  "Subtasks" with a clickable button for the parent ID. Uses the same navigation
  handler as subtask links.

See `web/CLAUDE.md` → "Subtask parent navigation" for styling details.

The frontmatter is delimited by `---` lines. The body is freeform markdown. When
parsing, split on `---` — first element is empty (before opening delimiter),
second is YAML, third is body.

## Go type definitions

These are the authoritative struct definitions.

```go
// internal/board/card.go

type Card struct {
    ID             string          `yaml:"id"              json:"id"`
    Title          string          `yaml:"title"           json:"title"`
    Project        string          `yaml:"project"         json:"project"`
    Type           string          `yaml:"type"            json:"type"`
    State          string          `yaml:"state"           json:"state"`
    Priority       string          `yaml:"priority"        json:"priority"`
    AssignedAgent  string          `yaml:"assigned_agent,omitempty"  json:"assigned_agent,omitempty"`
    LastHeartbeat  *time.Time      `yaml:"last_heartbeat,omitempty" json:"last_heartbeat,omitempty"`
    Parent         string          `yaml:"parent,omitempty"         json:"parent,omitempty"`
    Subtasks       []string        `yaml:"subtasks,omitempty"       json:"subtasks,omitempty"`
    DependsOn      []string        `yaml:"depends_on,omitempty"     json:"depends_on,omitempty"`
    DependenciesMet *bool          `yaml:"-"                        json:"dependencies_met,omitempty"`
    Context        []string        `yaml:"context,omitempty"        json:"context,omitempty"`
    Labels         []string        `yaml:"labels,omitempty"         json:"labels,omitempty"`
    Source         *Source         `yaml:"source,omitempty"         json:"source,omitempty"`
    Vetted         bool            `yaml:"vetted,omitempty"         json:"vetted,omitempty"`
    Custom         map[string]any  `yaml:"custom,omitempty"         json:"custom,omitempty"`
    Autonomous          bool            `yaml:"autonomous,omitempty"              json:"autonomous,omitempty"`
    UseOpusOrchestrator bool            `yaml:"use_opus_orchestrator,omitempty"   json:"use_opus_orchestrator,omitempty"`
    FeatureBranch       bool            `yaml:"feature_branch,omitempty"          json:"feature_branch,omitempty"`
    CreatePR       bool            `yaml:"create_pr,omitempty"      json:"create_pr,omitempty"`
    BranchName     string          `yaml:"branch_name,omitempty"    json:"branch_name,omitempty"`
    BaseBranch     string          `yaml:"base_branch,omitempty"    json:"base_branch,omitempty"`
    PRUrl          string          `yaml:"pr_url,omitempty"         json:"pr_url,omitempty"`
    ReviewAttempts int             `yaml:"review_attempts,omitempty" json:"review_attempts,omitempty"`
    RunnerStatus   string          `yaml:"runner_status,omitempty"  json:"runner_status,omitempty"`
    TokenUsage     *TokenUsage     `yaml:"token_usage,omitempty"    json:"token_usage,omitempty"`
    Created        time.Time       `yaml:"created"                  json:"created"`
    Updated        time.Time       `yaml:"updated"                  json:"updated"`
    ActivityLog    []ActivityEntry `yaml:"activity_log,omitempty"   json:"activity_log,omitempty"`
    Body           string          `yaml:"-"                        json:"body"`
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

type TokenUsage struct {
    Model            string  `yaml:"model,omitempty"    json:"model,omitempty"`
    PromptTokens     int64   `yaml:"prompt_tokens"      json:"prompt_tokens"`
    CompletionTokens int64   `yaml:"completion_tokens"  json:"completion_tokens"`
    EstimatedCostUSD float64 `yaml:"estimated_cost_usd" json:"estimated_cost_usd"`
}
```

```go
// internal/board/project.go

type RemoteExecutionConfig struct {
    Enabled     *bool  `yaml:"enabled,omitempty"      json:"enabled,omitempty"`
    RunnerImage string `yaml:"runner_image,omitempty"  json:"runner_image,omitempty"`
}

type GitHubImportConfig struct {
    ImportIssues    bool     `yaml:"import_issues"              json:"import_issues"`
    Owner           string   `yaml:"owner,omitempty"            json:"owner,omitempty"`
    Repo            string   `yaml:"repo,omitempty"             json:"repo,omitempty"`
    CardType        string   `yaml:"card_type,omitempty"        json:"card_type,omitempty"`
    DefaultPriority string   `yaml:"default_priority,omitempty" json:"default_priority,omitempty"`
    Labels          []string `yaml:"labels,omitempty"           json:"labels,omitempty"`
}

type JiraEpicConfig struct {
    EpicKey    string `yaml:"epic_key"    json:"epic_key"`
    ProjectKey string `yaml:"project_key" json:"project_key"`
}

type ProjectConfig struct {
    Name            string                 `yaml:"name"`
    Prefix          string                 `yaml:"prefix"`
    NextID          int                    `yaml:"next_id"`
    Repo            string                 `yaml:"repo,omitempty"`
    States          []string               `yaml:"states"`
    Types           []string               `yaml:"types"`
    Priorities      []string               `yaml:"priorities"`
    Transitions     map[string][]string    `yaml:"transitions"`
    RemoteExecution *RemoteExecutionConfig `yaml:"remote_execution,omitempty"`
    GitHub          *GitHubImportConfig    `yaml:"github,omitempty"`
    Jira            *JiraEpicConfig        `yaml:"jira,omitempty"`
    Templates       map[string]string      `yaml:"-"` // loaded from templates/ dir at runtime
}
```

**Immutable fields** (set on creation, never changed): `id`, `project`,
`created`, `source`. Additionally, `branch_name` is immutable after first
generation.

**Server-managed fields** (set by service layer, not by clients directly): `id`,
`created`, `updated`, `assigned_agent`, `last_heartbeat`, `activity_log`,
`runner_status`, `review_attempts`, `branch_name`, `token_usage`,
`dependencies_met`.

**Human-only fields** (may only be set by agents whose `X-Agent-ID` starts with
`human:`): `vetted`, `autonomous`, `feature_branch`, `create_pr`, `base_branch`.
Agents that attempt to set these fields receive 403 `HUMAN_ONLY_FIELD`. The MCP
`update_card` tool does not expose them.

## Reserved labels

Most labels are free-form, but the following have built-in meaning:

| Label    | Effect                                                                                                                                                                                                                                                                                                                                                                             |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `simple` | Autonomous fast path. When a card has this label **and** no existing subtasks, `run-autonomous` skips planning, subtask creation, review, and documentation — executing the work directly and transitioning to `done`. Claims, heartbeats, tests, branch protection, and release are still enforced. Classified server-side in `classifyComplexity()` (`internal/mcp/prompts.go`). |

## Card body templates

Templates live in `<project>/templates/<type>.md` in the boards repo. The
filename without `.md` must exactly match the card type (e.g. `task.md` for type
`task`). Templates are plain markdown with no YAML frontmatter.

The server loads all files in the `templates/` directory at startup (and on
project reload) and stores them in `ProjectConfig.Templates` keyed by type name.
They are returned to agents via `get_task_context` and surfaced in API responses
as part of the project config.

**Type-scoped loading in the web UI (`CreateCardForm`):**

| Condition                            | Behaviour                                                         |
| ------------------------------------ | ----------------------------------------------------------------- |
| Type has a template, body not dirty  | Template content is loaded into the body editor automatically     |
| Type has a template, body IS dirty   | User is prompted to confirm before the template replaces the body |
| Type has no template, body not dirty | Body editor is cleared                                            |
| Type has no template, body IS dirty  | Body is left unchanged — user content is never silently discarded |

The `bodyDirty` flag is set as soon as the user edits the body editor. It is
cleared when a template is accepted (either automatically or after
confirmation). This ensures template auto-loading is only applied to unedited
content.

## Project board config format

```yaml
# boards/project-alpha/.board.yaml
name: project-alpha
prefix: ALPHA
next_id: 1
repo: git@github.com:org/project-alpha.git
states: [todo, in_progress, blocked, review, done, stalled, not_planned]
types: [task, bug, feature] # "subtask" is built-in — do not add it here
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

Optional integration fields:

```yaml
# Set automatically when importing a Jira epic (not user-edited)
jira:
  epic_key: PROJ-42
  project_key: PROJ
```

### Jira field mapping

When importing a Jira epic, child issue fields are mapped to CM card fields as
follows.

**Priority mapping** (`internal/jira/priority.go`):

| Jira priority                         | CM priority |
| ------------------------------------- | ----------- |
| Highest, Critical, Blocker            | critical    |
| High                                  | high        |
| Medium, Normal                        | medium      |
| Low, Lowest, Trivial, Minor          | low         |
| Unknown or empty                      | medium      |

**Issue type mapping** (`internal/jira/importer.go`):

| Jira issue type         | CM card type |
| ----------------------- | ------------ |
| Bug                     | bug          |
| Story, Task, Sub-task   | task         |
| Improvement, New Feature| feature      |
| Everything else         | task         |

**Other mappings:**

- Card title is `"<issue key> <summary>"` (e.g., `"PROJ-43 Implement feature"`).
  The issue key prefix makes the Jira origin visible at a glance in the board UI.
- Jira labels + component names are merged into the CM card's `labels` field.
- Jira description (plain text or ADF) is extracted as plain text into the CM
  card body. Rich formatting (tables, macros, embedded media) is not preserved.
- All imported cards have `vetted: true` (human-initiated import is considered
  vetted).
- `source.system` is set to `"jira"`, `source.external_id` to the Jira issue
  key, and `source.external_url` to the browse URL for the issue.

Both `stalled` and `not_planned` must always be present in `states` and
`transitions`. The server enforces this. All other states are optional in the
`transitions` map — a state with no entry is a valid terminal state (no outgoing
transitions). For example, omitting `done` from the transitions map makes it
truly terminal, while including `done: [todo]` allows re-opening cards. Any
state can transition to `stalled` without being listed in the source state's
transitions — the server injects this automatically (needed for heartbeat
timeout). `not_planned` follows normal transition rules: only states that
explicitly list `not_planned` in their transitions can reach it (e.g.,
`todo: [in_progress, not_planned]`).
