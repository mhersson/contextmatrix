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

13. **Card deletion requires no subtasks.** `DELETE /api/projects/{project}/cards/{id}`
    (`git rm` + commit) is rejected with 422 `VALIDATION_ERROR` if the card has
    any subtasks. Delete all subtasks first. Deletion of a claimed card also
    requires the `X-Agent-ID` header to match `assigned_agent` (403 on
    mismatch). The web UI enforces a softer gate: the Delete button is enabled
    only when the card is in `todo` or `not_planned` state **and** has no
    `assigned_agent`. A native confirmation dialog warns the user that the
    action is irreversible and commits the removal to git.

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

### Runner-orchestration fields (optional)

Cards driven by the runner-orchestrator agent carry a set of optional fields
that track multi-repo intent, plan/review approval gates, brainstorm output, and
push records. All fields are `omitempty`; cards that do not use the runner
workflow simply omit them.

| Field              | YAML                 | Type                | Purpose                                                                  |
| ------------------ | -------------------- | ------------------- | ------------------------------------------------------------------------ |
| Repos              | `repos`              | `[]string`          | Hint at which repos the card touches; not authoritative.                 |
| ChosenRepos        | `chosen_repos`       | `[]string`          | Set authoritatively after the plan phase.                                |
| BlockerCards       | `blocker_cards`      | `[]string`          | IDs of subtask cards this card depends on.                               |
| RevisionAttempts   | `revision_attempts`  | `int`               | Bumped each time a plan is rejected.                                     |
| RevisionRequested  | `revision_requested` | `bool`              | Set when a human requests plan revision.                                 |
| PlanApproved       | `plan_approved`      | `bool`              | HITL plan-gate approval.                                                 |
| ReviewApproved     | `review_approved`    | `bool`              | HITL review-gate approval.                                               |
| DiscoveryComplete  | `discovery_complete` | `bool`              | Brainstorm phase fired the `discovery_complete` tool.                    |
| AgentSessions      | `agent_sessions`     | `map[string]string` | Stable per-purpose Claude session IDs (purpose → session_id).            |
| DocsWritten        | `docs_written`       | `bool`              | Documentation phase has run.                                             |
| PushRecords        | `push_records`       | `[]PushRecord`      | One entry per repo branch push (multi-repo).                             |

`PushRecord` has the shape `{repo, branch, pr_url, pushed_at}` — one record per
push to a repo branch (so a multi-repo card may accumulate several entries).

Example card with several runner-orchestration fields populated:

```yaml
---
id: PAY-042
title: Move billing webhook signing to KMS
project: pay-q3
type: task
state: review
priority: high
repos: [billing-svc, auth-svc]
chosen_repos: [billing-svc]
blocker_cards: [PAY-043, PAY-044]
revision_attempts: 1
revision_requested: false
plan_approved: true
review_approved: false
discovery_complete: true
agent_sessions:
  plan: sess_01HXY...
  execute: sess_01HZA...
docs_written: true
push_records:
  - repo: billing-svc
    branch: feat/PAY-042-kms-signing
    pr_url: https://github.com/org/billing-svc/pull/118
    pushed_at: 2026-04-21T09:14:00Z
created: 2026-04-20T08:00:00Z
updated: 2026-04-21T09:14:00Z
---
```

### `skills` (optional, `*[]string`)

List of task-skill names mounted into the worker container's `~/.claude/skills/` directory. Three states:

- **field absent** (`nil`): inherit from project's `default_skills`, or mount the full curated set if that's also unset.
- **`skills: []`**: explicit "no specialist skills for this card." Container's `~/.claude/skills/` is empty.
- **`skills: [name1, name2]`**: constrain to this list. Only these are mounted.

**Inheritance:** When a subtask is created with `parent` set and no `skills` of its own, the parent's `skills` value is copied onto the subtask at creation time (one-shot). Later edits to the parent do not propagate. Pass `skills` explicitly to `create_card` to override.

**Override path:** The `skills` field can be set via `update_card` MCP tool, the REST `PATCH` endpoint, or by hand-editing the YAML. The web UI multi-select is a follow-up feature.

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

type ProjectConfig struct {
    Name            string                 `yaml:"name"`
    DisplayName     string                 `yaml:"display_name,omitempty"`
    Prefix          string                 `yaml:"prefix"`
    NextID          int                    `yaml:"next_id"`
    Repo            string                 `yaml:"repo,omitempty"`
    States          []string               `yaml:"states"`
    Types           []string               `yaml:"types"`
    Priorities      []string               `yaml:"priorities"`
    Transitions     map[string][]string    `yaml:"transitions"`
    RemoteExecution *RemoteExecutionConfig `yaml:"remote_execution,omitempty"`
    GitHub          *GitHubImportConfig    `yaml:"github,omitempty"`
    DefaultSkills   *[]string              `yaml:"default_skills,omitempty"` // nil=full set, []=none, [list]=constrain
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
display_name: "Project Alpha"   # optional — human-readable name shown in the UI
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
# default_skills: [go-development, documentation]  # optional — see below
```

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

### `default_skills` (optional, `*[]string`)

Project-wide fallback when a card has no `skills` field of its own. Same three-state semantics. A card's explicit `skills` (including explicit empty) overrides this.

### `jira_project_key` (optional, `string`)

Links the CM project to a Jira project so that the KB tier 2 file at
`_kb/jira-projects/<KEY>.md` (e.g. `_kb/jira-projects/PAY.md`) is loaded into
`ProjectKB.JiraProject`. Has no effect when unset.

### `repos` (optional, `[]RepoSpec`)

Registry of repos this project's cards may touch. Each `RepoSpec` is:

| Field         | YAML          | Type     | Purpose                                                                |
| ------------- | ------------- | -------- | ---------------------------------------------------------------------- |
| Slug          | `slug`        | `string` | Stable key used in `_kb/repos/<slug>.md` and card `chosen_repos` / `repos`. |
| URL           | `url`         | `string` | Clone URL (HTTPS or SSH).                                              |
| Description   | `description` | `string` | Optional human-readable note.                                          |

**Backward compatibility.** A project that uses the legacy single-string `repo:
<url>` field is automatically expanded at load time into a one-entry `repos`
registry whose `slug` is derived from the URL's last path segment with `.git`
stripped (e.g. `git@github.com:org/auth-svc.git` → slug `auth-svc`). The legacy
`repo` field stays populated so existing consumers keep working.

Example using the new registry:

```yaml
name: pay-q3
prefix: PAY
next_id: 1
jira_project_key: PAY
repos:
  - slug: billing-svc
    url: git@github.com:org/billing-svc.git
    description: "Stripe webhook receiver and ledger writer"
  - slug: auth-svc
    url: git@github.com:org/auth-svc.git
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

Equivalent legacy shape (auto-expanded to the same one-entry `repos` registry):

```yaml
name: pay-q3
prefix: PAY
next_id: 1
repo: git@github.com:org/billing-svc.git
# states/types/priorities/transitions as above
```

## Knowledge base (`_kb/`) layout

The boards repo carries a tiered, file-based knowledge base that the runner
agent loads as system-prompt context. Three tiers:

- **`_kb/repos/<slug>.md`** — long-lived per-repo notes. The `<slug>` is matched
  against `ProjectConfig.Repos[].Slug`; only repos listed in the project's
  registry are loaded.
- **`_kb/jira-projects/<KEY>.md`** — long-lived per-Jira-project notes. The
  `<KEY>` is matched against `ProjectConfig.JiraProjectKey`; the file is loaded
  only when the project sets that field.
- **`<project>/kb/project.md`** — ephemeral per-CM-project notes living inside
  the project directory. Disappears when the CM project (epic) is deleted.

The `get_project_kb` MCP tool merges the layers into a `ProjectKB` value:

```go
type ProjectKB struct {
    Repos       map[string]string // slug → file body
    JiraProject string
    Project     string
}
```

`ProjectKB.RenderMarkdown()` emits sections in a stable order: each repo under
`## Repository: <slug>` (lexicographic by slug), then `## Jira project`, then
`## Project`. Missing tiers are silently omitted. An empty KB renders to the
empty string.

Example tree:

```
boards/
├─ _kb/
│  ├─ repos/
│  │  ├─ auth-svc.md
│  │  └─ billing-svc.md
│  └─ jira-projects/
│     └─ PAY.md
└─ pay-q3/
   ├─ .board.yaml
   ├─ tasks/
   └─ kb/
      └─ project.md
```
