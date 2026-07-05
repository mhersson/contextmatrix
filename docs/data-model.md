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

3. **One agent per card.** `POST /api/projects/{project}/cards/{id}/claim` fails
   with 409 if card is already claimed. Only the assigned agent can mutate a
   claimed card — API checks `X-Agent-ID` header against `assigned_agent` and
   returns 403 on mismatch. Unclaimed cards can be mutated by anyone.

4. **Human identity.** Humans use agent IDs prefixed with `human:` (e.g.,
   `human:alice`). The claim system treats them identically to AI agents. The
   web UI stores the human's agent ID in localStorage and sends it via
   `X-Agent-ID` header.

5. **Every mutation auto-commits (with optional deferral).** The service layer
   writes the file, then commits via `GitManager`. Commit message format:
   `[contextmatrix] CARD-ID: description` or
   `[agent:AGENT-ID] CARD-ID: description`. When
   `boards.git_deferred_commit: true` in `config.yaml`, agent mutations during a
   work session are batched and flushed as a single commit at claim
   release/completion. Card creation and human edits to unclaimed cards are
   always committed immediately regardless of this setting.

6. **Activity log is append-only, capped at 50 entries.** Agents add entries via
   the MCP `add_log` tool. Older entries beyond 50 are dropped from the card
   file but preserved in git history. Entries are never edited or deleted.

7. **Heartbeat timeout.** If `last_heartbeat` exceeds configured timeout
   (default 30min), the service layer (`CardService.StartTimeoutChecker` in
   `internal/service/service_locks.go`) periodically scans for stalled cards,
   sets each one's state to `stalled`, clears `assigned_agent`, commits to git,
   and publishes a `CardStalled` event. The lock manager's role is limited to
   enumeration: `Manager.FindStalled` returns the candidate list and never
   mutates cards. The state change, persistence, commit, and event publication
   are all owned by the service layer.

8. **External source tracking.** Cards imported from external systems (Jira,
   GitHub Issues, etc.) use the `source` field to record origin. The
   `source.external_id` field is indexed and queryable via
   `GET /api/projects/{project}/cards?external_id=PROJ-1234`. This provides
   idempotent imports — check if the external ID exists before creating, update
   if it does. The `source` field is immutable after creation.
   `source.external_url`, when present, must use an `http` or `https` scheme —
   any other scheme (e.g. `javascript:`, `data:`, `vbscript:`) is rejected at
   write time with a 422 validation error (`ErrInvalidExternalURL`).

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
   - The web UI shows an "unvetted" badge on board cards and a warning banner in
     the card panel for cards with `source && !vetted`.

10. **Parent card auto-transitions on child state changes.** When a subtask
    actually transitions to `in_progress` (via `UpdateCard`, `PatchCard`, or a
    transition through the state machine), the service layer automatically
    transitions the parent from `todo` → `in_progress` if it is currently in
    `todo`. The `claim` operation by itself does **not** trigger this —
    `Manager.Claim` sets `assigned_agent` and `last_heartbeat` but never changes
    `state`, so an agent that claims a `todo` subtask without moving it to
    `in_progress` will not bump the parent. When all subtasks reach `done`, the
    parent stays in `in_progress` — the orchestrator spawns a documentation
    sub-agent first, then manually transitions the parent to `review`. The
    `complete_task` MCP tool detects when all siblings are done and returns an
    informational message so the calling agent knows documentation can proceed.

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
    created. The response is identical in shape to a normal create response (201
    Created with the card body — the `createCard` handler unconditionally
    returns 201 regardless of whether the card was newly created or matched an
    existing duplicate), so callers do not need to handle this case specially.

    Rationale: LLM agents may re-enter Phase 2 (subtask creation) after a crash
    or context reset, causing the same subtask to be created twice with
    sequential IDs. The guard prevents orphaned duplicate cards without
    requiring callers to check first.
    - The check is under `writeMu`, so there is no TOCTOU race.
    - The `next_id` counter is still incremented and the gap is harmless.
    - If an identically-titled subtask exists but is already `done` or
      `not_planned`, a new card **is** created — duplicates of completed work
      are intentional (e.g., re-doing a failed step).

13. **Card deletion requires no subtasks.**
    `DELETE /api/projects/{project}/cards/{id}` (filesystem `os.Remove` of the
    card file followed by an enqueued commit via `commitQueue.Enqueue` with
    `CommitKindFile`; go-git records the deletion when the missing path is
    staged — there is no `git rm` invocation) is rejected with 422
    `VALIDATION_ERROR` if the card has any subtasks. Delete all subtasks first.
    Deletion of a claimed card also requires the `X-Agent-ID` header to match
    `assigned_agent` (403 on mismatch). The web UI enforces a softer gate: the
    Delete button is enabled only when the card is in `todo` or `not_planned`
    state **and** has no `assigned_agent`. A custom `ConfirmModal` React
    component (see `web/CLAUDE.md` → "ConfirmModal") warns the user that the
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
subtasks: [ALPHA-003, ALPHA-004] # operator-maintained — set by callers via UpdateCard; not auto-populated when subtasks are created
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
branch_name: alpha-001/implement-user-auth
base_branch: develop
pr_url: https://github.com/org/repo/pull/42
review_attempts: 0
runner_status: ""
token_usage:
  model: claude-sonnet-4-6
  prompt_tokens: 12340
  completion_tokens: 5670
  cache_read_tokens: 80000     # optional, omitted when zero
  cache_creation_tokens: 4000  # optional, omitted when zero
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
set. The server enforces this automatically (see domain rule 11):

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

### `skills` (optional, `*[]string`)

List of task-skill names mounted into the worker container's `~/.claude/skills/`
directory. Three states:

- **field absent** (`nil`): inherit from project's `default_skills`, or mount
  the full curated set if that's also unset.
- **`skills: []`**: explicit "no specialist skills for this card." Container's
  `~/.claude/skills/` is empty.
- **`skills: [name1, name2]`**: constrain to this list. Only these are mounted.

**Inheritance:** When a subtask is created with `parent` set and no `skills` of
its own, the parent's `skills` value is copied onto the subtask at creation time
(one-shot). Later edits to the parent do not propagate. Pass `skills` explicitly
to `create_card` to override.

**Override path:** The `skills` field can be set via `update_card` MCP tool, the
REST `PATCH` endpoint, hand-editing the YAML, or the per-card multi-select in
the CardPanel metadata (`MetadataSkills`). Project-wide defaults are managed via
the `DefaultSkillsSelector` in Project Settings.

## Go type definitions

These are the authoritative struct definitions.

```go
// internal/board/card.go

type Card struct {
    ID                  string          `yaml:"id"                              json:"id"`
    Title               string          `yaml:"title"                           json:"title"`
    Project             string          `yaml:"project"                         json:"project"`
    Type                string          `yaml:"type"                            json:"type"`
    State               string          `yaml:"state"                           json:"state"`
    Priority            string          `yaml:"priority"                        json:"priority"`
    AssignedAgent       string          `yaml:"assigned_agent,omitempty"        json:"assigned_agent,omitempty"`
    LastHeartbeat       *time.Time      `yaml:"last_heartbeat,omitempty"        json:"last_heartbeat,omitempty"`
    Parent              string          `yaml:"parent,omitempty"                json:"parent,omitempty"`
    Subtasks            []string        `yaml:"subtasks,omitempty"              json:"subtasks,omitempty"`
    DependsOn           []string        `yaml:"depends_on,omitempty"            json:"depends_on,omitempty"`
    DependenciesMet     *bool           `yaml:"-"                               json:"dependencies_met,omitempty"`
    Context             []string        `yaml:"context,omitempty"               json:"context,omitempty"`
    Labels              []string        `yaml:"labels,omitempty"                json:"labels,omitempty"`
    Skills              *[]string       `yaml:"skills,omitempty"                json:"skills,omitempty"`
    Source              *Source         `yaml:"source,omitempty"                json:"source,omitempty"`
    Custom              map[string]any  `yaml:"custom,omitempty"                json:"custom,omitempty"`
    Autonomous          bool            `yaml:"autonomous,omitempty"            json:"autonomous"`
    UseOpusOrchestrator bool            `yaml:"use_opus_orchestrator,omitempty" json:"use_opus_orchestrator,omitempty"`
    ModelOrchestrator   string          `yaml:"model_orchestrator,omitempty"    json:"model_orchestrator,omitempty"`
    ModelCoder          string          `yaml:"model_coder,omitempty"           json:"model_coder,omitempty"`
    ModelReviewer       string          `yaml:"model_reviewer,omitempty"        json:"model_reviewer,omitempty"`
    BestOfN             int             `yaml:"best_of_n,omitempty"             json:"best_of_n,omitempty"`
    Vetted              bool            `yaml:"vetted,omitempty"                json:"vetted"`
    FeatureBranch       bool            `yaml:"feature_branch,omitempty"        json:"feature_branch,omitempty"`
    CreatePR            bool            `yaml:"create_pr,omitempty"             json:"create_pr,omitempty"`
    BranchName          string          `yaml:"branch_name,omitempty"           json:"branch_name,omitempty"`
    BaseBranch          string          `yaml:"base_branch,omitempty"           json:"base_branch,omitempty"`
    PRUrl               string          `yaml:"pr_url,omitempty"                json:"pr_url,omitempty"`
    ReviewAttempts      int             `yaml:"review_attempts,omitempty"       json:"review_attempts,omitempty"`
    RunnerStatus        string          `yaml:"runner_status,omitempty"         json:"runner_status,omitempty"`
    Phase               string          `yaml:"phase,omitempty"                 json:"phase,omitempty"`
    TokenUsage          *TokenUsage     `yaml:"token_usage,omitempty"           json:"token_usage,omitempty"`
    UsageBreakdown      []UsageBucket   `yaml:"usage_breakdown,omitempty"       json:"usage_breakdown,omitempty"`
    Created             time.Time       `yaml:"created"                         json:"created"`
    Updated             time.Time       `yaml:"updated"                         json:"updated"`
    ActivityLog         []ActivityEntry `yaml:"activity_log,omitempty"          json:"activity_log,omitempty"`
    Body                string          `yaml:"-"                               json:"body"`
}

// Note: Autonomous and Vetted intentionally use `json:"autonomous"` /
// `json:"vetted"` (no `omitempty`) so the boolean is always emitted in API
// responses — clients can distinguish "explicitly false" from "field not
// returned". Other booleans (FeatureBranch, CreatePR, UseOpusOrchestrator)
// keep `omitempty` because they are opt-in and absence carries no meaning.

type ActivityEntry struct {
    Agent     string    `yaml:"agent"           json:"agent"`
    Timestamp time.Time `yaml:"ts"              json:"ts"`
    Action    string    `yaml:"action"          json:"action"`
    Message   string    `yaml:"message"         json:"message"`
    Skill     string    `yaml:"skill,omitempty" json:"skill,omitempty"` // set on `skill_engaged` actions
}

type Source struct {
    System      string `yaml:"system"       json:"system"`
    ExternalID  string `yaml:"external_id"  json:"external_id"`
    ExternalURL string `yaml:"external_url" json:"external_url"`
}

type TokenUsage struct {
    Model               string  `yaml:"model,omitempty"                 json:"model,omitempty"`
    PromptTokens        int64   `yaml:"prompt_tokens"                   json:"prompt_tokens"`
    CompletionTokens    int64   `yaml:"completion_tokens"               json:"completion_tokens"`
    CacheReadTokens     int64   `yaml:"cache_read_tokens,omitempty"     json:"cache_read_tokens,omitempty"`
    CacheCreationTokens int64   `yaml:"cache_creation_tokens,omitempty" json:"cache_creation_tokens,omitempty"`
    EstimatedCostUSD    float64 `yaml:"estimated_cost_usd"              json:"estimated_cost_usd"`
}

type UsageBucket struct {
    Agent               string  `yaml:"agent"                           json:"agent"`
    Model               string  `yaml:"model"                           json:"model"`
    PromptTokens        int64   `yaml:"prompt_tokens"                   json:"prompt_tokens"`
    CompletionTokens    int64   `yaml:"completion_tokens"               json:"completion_tokens"`
    CacheReadTokens     int64   `yaml:"cache_read_tokens,omitempty"     json:"cache_read_tokens,omitempty"`
    CacheCreationTokens int64   `yaml:"cache_creation_tokens,omitempty" json:"cache_creation_tokens,omitempty"`
    CostUSD             float64 `yaml:"cost_usd"                        json:"cost_usd"`
    CostSource          string  `yaml:"cost_source"                     json:"cost_source"`
}
```

`CacheReadTokens` and `CacheCreationTokens` are optional (`omitempty`); they are
absent from the YAML/JSON when zero (cards whose agents do not pass cache
fields). `RecalculateCosts` handles
absent values correctly — missing fields default to 0 and do not affect the
recalculated cost.

**Cost formula** (applied per `report_usage` call and by `RecalculateCosts`):

```
estimated_cost_usd +=
    prompt_tokens         * rate.Prompt
  + cache_read_tokens     * rate.Prompt * 0.10
  + cache_creation_tokens * rate.Prompt * 1.25
  + completion_tokens     * rate.Completion
```

`cache_creation_tokens` uses a single 1.25× multiplier, collapsing the 5-minute
and 1-hour cache-write tiers. Claude Code uses the 5-minute tier by default.
Agents should pass the `cache_creation_input_tokens` field from Claude's
stream-json `usage` frame directly — no tier distinction is required.

### Usage breakdown

`UsageBreakdown` holds one `UsageBucket` per `(agent, model)` pair, merging every
`report_usage` call for that pair into a single row. It exists to attribute cost
after a card is released (when `assigned_agent` is cleared) and across multiple
agents or models on one card. Empty-agent buckets roll up to the dashboard's
`unassigned` label.

`cost_source` is `actual` when the bucket's cost came from the provider (passed
on `report_usage` as `actual_cost_usd`) or `estimated` when it was priced from
the local rate table. **Actual is authoritative and is never re-priced** —
`RecalculateCosts` re-prices only `estimated` buckets from the current rate
table and leaves `actual` buckets untouched. A bucket that has ever received an
actual-cost report stays `actual`.

The cumulative `TokenUsage` (counters and `estimated_cost_usd`) is kept equal to
the bucket sum for breakdown cards: each report increments both the matching
bucket and the cumulative total. Cards with no `usage_breakdown` buckets fall
back to `assigned_agent` for their agent rollup.

```go
// internal/board/project.go

type Repo struct {
    Name    string `yaml:"name"              json:"name"`
    URL     string `yaml:"url"               json:"url"`
    Primary bool   `yaml:"primary,omitempty" json:"primary,omitempty"`
}

type RemoteExecutionConfig struct {
    Enabled     *bool  `yaml:"enabled,omitempty"      json:"enabled,omitempty"`
    RunnerImage string `yaml:"runner_image,omitempty" json:"runner_image,omitempty"`
}

type GitHubImportConfig struct {
    ImportIssues    bool     `yaml:"import_issues"              json:"import_issues"`
    Owner           string   `yaml:"owner,omitempty"            json:"owner,omitempty"`
    Repo            string   `yaml:"repo,omitempty"             json:"repo,omitempty"`
    CardType        string   `yaml:"card_type,omitempty"        json:"card_type,omitempty"`
    DefaultPriority string   `yaml:"default_priority,omitempty" json:"default_priority,omitempty"`
    Labels          []string `yaml:"labels,omitempty"           json:"labels,omitempty"`
}

type ProjectConfig struct {
    Name             string                   `yaml:"name"                        json:"name"`
    DisplayName      string                   `yaml:"display_name,omitempty"      json:"display_name,omitempty"`
    Prefix           string                   `yaml:"prefix"                      json:"prefix"`
    NextID           int                      `yaml:"next_id"                     json:"next_id"`
    Repo             string                   `yaml:"repo,omitempty"              json:"repo,omitempty"`               // singular repo; Repos is the multi-repo form and takes precedence
    Repos            []Repo                   `yaml:"repos,omitempty"             json:"repos,omitempty"`              // multi-repo projects; one entry may be Primary
    GitHubCredential string                   `yaml:"github_credential,omitempty" json:"github_credential,omitempty"` // instance credential-pool entry name; empty = instance github.* credential
    States           []string                 `yaml:"states"                      json:"states"`
    Types            []string                 `yaml:"types"                       json:"types"`
    Priorities       []string                 `yaml:"priorities"                  json:"priorities"`
    Transitions      map[string][]string      `yaml:"transitions"                 json:"transitions"`
    RemoteExecution  *RemoteExecutionConfig   `yaml:"remote_execution,omitempty"  json:"remote_execution,omitempty"`
    GitHub           *GitHubImportConfig      `yaml:"github,omitempty"            json:"github,omitempty"`
    DefaultSkills    *[]string                `yaml:"default_skills,omitempty"    json:"default_skills,omitempty"` // nil=full set, []=none, [list]=constrain
    Favorites        map[string]TierFavorites `yaml:"favorites,omitempty"         json:"-"`                        // per-project tier overrides for the agent-backend model selector; merged with global at trigger time
    Templates        map[string]string        `yaml:"-"                           json:"templates,omitempty"`      // loaded from templates/ dir at runtime
}
```

`ProjectConfig.EffectiveRepos()` normalises the two fields into a single
`[]Repo`: when `Repos` is set the slice is returned with empty `Name` fields
derived from `URL` and the first entry auto-promoted to `Primary` when no entry
sets it; otherwise the singular `Repo` field is synthesised as
`[]Repo{{URL: Repo, Primary: true}}`. Validation rejects duplicate names,
missing URLs, or more than one `Primary: true` entry.

`Favorites` holds per-project overrides for the agent-backend leaderboard
model selector, keyed by complexity tier. Each `board.TierFavorites` value is
either a bare list of preferred model slugs (applies to every role) or a
`{coder: [...], reviewer: [...]}` map narrowing to one role — see the
`favorites:` example in `config.yaml.example`'s `backends.agent` block, which
documents the same shape for the instance-wide default. `mergeFavorites`
(`internal/api/runner.go`) combines the backend's global
`backends.agent.favorites` with a project's `Favorites` at trigger time, with
project entries taking priority per tier. `json:"-"` — there is no REST
create/update path for this field; set it by hand-editing `.board.yaml`.

**Immutable fields** (set on creation, never changed): `id`, `project`,
`created`, `source`. Additionally, `branch_name` is immutable after first
generation.

**Server-managed fields** (set by service layer, not by clients directly): `id`,
`created`, `updated`, `assigned_agent`, `last_heartbeat`, `activity_log`,
`runner_status`, `review_attempts`, `branch_name`, `token_usage`,
`usage_breakdown`, `dependencies_met`.

**Agent-managed field** — `phase`: the agent-orchestrator's progress within a run
(`plan` | `execute` | `judge` | `document` | `review` | `integrate` | `done`), orthogonal
to `state`. `judge` is exercised only during a Best-of-N run (see `best_of_n`
below) — the agent-backend orchestrator selects a winner among the racing
candidates there before continuing to `document`; it is a no-op phase for
normal runs. Enum-validated; the empty string clears it and means "not agent-driven". Settable
via the `update_card` MCP tool and REST (PUT/PATCH).

**Human-only fields** (may only be set by agents whose `X-Agent-ID` starts with
`human:`): `vetted`, `autonomous`, `use_opus_orchestrator`, `feature_branch`,
`create_pr`, the three model pins (`model_orchestrator`, `model_coder`,
`model_reviewer`), `base_branch`, and `best_of_n`. POST `/api/projects/{project}/cards`
(`createCardRequest`) and PUT `/api/projects/{project}/cards/{id}`
(`updateCardRequest`) gate the first five fields plus the model pins;
`base_branch` is **only exposed via PATCH** (`patchCardRequest`) — there is no
`base_branch` field on the create or full-update request bodies, so the
human-only check for it applies only on PATCH. The model pins are gated on
create, full-update, and PATCH. `best_of_n` is exposed on PUT and PATCH only —
like `base_branch` it has no field on `createCardRequest` — and, independent
of the human-only gate, is range-validated to `0` (off) or
`2..best_of_n.max_candidates`; a value outside that range is rejected with 400
`BAD_REQUEST` regardless of caller. Like the model pins, it is sticky: there
is no per-trigger override, so the card's stored value applies to every
subsequent run until a human changes or clears it, and it has effect only on
the agent backend (see `docs/remote-execution.md`). Agents that attempt to set
any of these fields receive 403 `HUMAN_ONLY_FIELD`. The MCP `update_card` tool
does not expose them.

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

**Type-scoped loading in the web UI (`CreateCardPanel`):**

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
display_name: "Project Alpha" # optional — human-readable name shown in the UI
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

**State names are part of the contract.** In addition to the validator's
`stalled` / `not_planned` requirement, the strings `todo`, `in_progress`,
`review`, and `done` are hardcoded into MCP tools and service-layer behaviour
(`claim_card` auto-transitions `todo → in_progress`; `complete_task` moves
subtasks to `done` and parents to `review`; parent auto-transitions key off
`todo` and `in_progress`; dashboard metrics filter on `done`/`stalled`/
`not_planned`). The validator does not enforce these four, but renaming them
will silently break the lifecycle. Add new states freely; do not rename the
built-in six. See the README's "States, Transitions, and Skills" section for
the full list and rationale.

### `default_skills` (optional, `*[]string`)

Project-wide fallback when a card has no `skills` field of its own. Same
three-state semantics. A card's explicit `skills` (including explicit empty)
overrides this.

### `github_credential` (optional, `string`)

Name of an instance credential-pool entry used for all of this project's
GitHub operations (branch listing, issue import sync). Reference only — never
secret material; the token itself lives in the credential pool and is
resolved server-side through `TokenProviderFor`. Empty or omitted means the
project uses the instance-wide `github.*` credential — the only option in
`auth.mode: none` (there is no credential pool there), and the default for
unbound projects in `auth.mode: multi` too. Admin-only to set — see
`PUT /api/projects/{project}` in `docs/api-reference.md`. Validated against
the credential pool on write in `auth.mode: multi` (unknown name → 422
`VALIDATION_ERROR`); in `auth.mode: none` a non-empty binding is rejected
outright (422) rather than silently falling back to the instance credential.

## Server-side field-length limits

The service layer enforces conservative size caps on user-supplied string and
slice fields to prevent abuse and runaway growth. Violations are returned as 422
`VALIDATION_ERROR` with `field` set to the offending key. Values are defined as
constants in `internal/service/service.go`:

| Field / dimension         | Limit      | Notes                             |
| ------------------------- | ---------- | --------------------------------- |
| `title`                   | 500 chars  | `maxTitleLen`                     |
| `body`                    | 512 KB     | `maxBodyLen` (`512 * 1024` bytes) |
| individual label          | 100 chars  | `maxLabelLen`                     |
| `labels` slice length     | 50 entries | `maxLabels`                       |
| `agent_id` / `X-Agent-ID` | 256 chars  | `maxAgentIDLen`                   |
| `activity_log[].message`  | 2000 chars | `maxLogMessage`                   |
| `activity_log[].action`   | 200 chars  | `maxLogAction`                    |

Activity log entries beyond the per-card cap of 50 are dropped (oldest first)
when a new entry is appended — they are not rejected at write time. See domain
rule 6.

## `runner_status` enum

`Card.RunnerStatus` is a small enum tracked by the service layer and the runner.
The full set of valid values lives in `internal/board/validation.go`'s
`validRunnerStatuses`:

| Value        | Set by                | Meaning                                                                       |
| ------------ | --------------------- | ----------------------------------------------------------------------------- |
| `""` (empty) | service layer / human | No runner attached. Default on newly-created cards and after terminal states. |
| `queued`     | service layer         | A runner has been requested but the container has not yet started.            |
| `running`    | runner callback       | The runner container is actively executing the task.                          |
| `failed`     | runner callback       | The runner exited with an error.                                              |
| `killed`     | service layer         | The runner was forcibly stopped by a server-initiated `stop` action.          |
| `completed`  | runner callback       | The runner finished successfully.                                             |

The runner-callback subset (`validRunnerCallbackStatuses`) is `running`,
`failed`, and `completed` — the runner cannot self-report `queued` or `killed`
because both are server-managed lifecycle states. Setting an invalid value
returns 422 `INVALID_RUNNER_STATUS`.

## `depends_on` cycle detection

`UpdateCard` and `PatchCard` reject changes that would introduce a circular
dependency between cards. After applying the requested `depends_on` set,
`detectDependencyCycle` walks the dependency graph from the card and reports any
back-edge. On a hit, the service returns a `ValidationError` wrapping
`ErrDependenciesNotMet` with `field: "depends_on"` and a message of the form
`"circular dependency detected: ALPHA-001 and ALPHA-007 depend on each other"`.
The check runs under `writeMu` to prevent two concurrent edits from racing into
a cycle.

## `feature_branch` and `create_pr` interaction

`Validator.ValidateAutonomousFields` (in `internal/board/validation.go`)
enforces a single combined invariant for the autonomous-execution boolean
fields:

> `create_pr: true` requires `feature_branch: true`.

A card with `create_pr: true` and `feature_branch: false` is rejected at write
time with 422 `VALIDATION_ERROR` (`ErrInvalidAutonomousConfig`,
`field: "create_pr"`). The reverse — `feature_branch: true` with
`create_pr: false` — is allowed; the runner will create and push the branch
without opening a pull request.

## `chat_sessions` SQLite schema

Chat session state is persisted in the shared `ops.db` operational store
(separate from the boards git repo and the images store; the same `ops.db` also
holds the model blacklist). The schema is created by `ensureSchema` in
`internal/opstore/sqlite/schema.go`, which runs `CREATE TABLE IF NOT EXISTS` DDL
for every table in its final shape. This is a clean-cut create: there is **no
migration ledger** (`schema_migrations`) and no backward-compat path — to change
the schema, edit the `ensureSchema` DDL. **Existing `chats.db` files from
earlier installs are not migrated; delete the obsolete file before upgrading.**
The `chat_messages` table additionally carries a `kind TEXT NOT NULL DEFAULT ''`
column (used for the Clear-Context divider).

**`chat_sessions` table:**

| Column                       | Type    | Default | Meaning                                                             |
| ---------------------------- | ------- | ------- | ------------------------------------------------------------------- |
| `id`                         | TEXT PK | —       | ULID-shaped session identifier.                                     |
| `title`                      | TEXT    | —       | Human-readable session name (auto-filled from first user message).  |
| `project`                    | TEXT    | —       | Associated project slug; empty for cross-project sessions.          |
| `status`                     | TEXT    | —       | Lifecycle state (`cold`, `active`, `warm-idle`, `ending`).          |
| `created_at`                 | INTEGER | —       | Unix epoch of session creation.                                     |
| `last_active`                | INTEGER | —       | Unix epoch of last activity; indexed for dashboard range queries.   |
| `created_by`                 | TEXT    | —       | Agent ID of the session creator.                                    |
| `container_id`               | TEXT    | NULL    | Runner container ID; cleared when the session goes cold.            |
| `workspace`                  | TEXT    | NULL    | JSON-encoded workspace directory list.                              |
| `model`                      | TEXT    | `''`    | Orchestrator model ID.                                              |
| `context_tokens`             | INTEGER | `0`     | Last context-window token count.                                    |
| `context_tokens_updated_at`  | INTEGER | NULL    | Unix epoch of last context-token update.                            |
| `rehydration_active`         | INTEGER | `0`     | Boolean flag for rehydration phase.                                 |
| `rehydration_started_at`     | INTEGER | NULL    | Unix epoch when rehydration started.                                |
| `prompt_tokens`              | INTEGER | `0`     | Cumulative input tokens from all usage frames.                      |
| `completion_tokens`          | INTEGER | `0`     | Cumulative output tokens.                                           |
| `cache_read_tokens`          | INTEGER | `0`     | Cumulative cache-read tokens.                                       |
| `cache_creation_tokens`      | INTEGER | `0`     | Cumulative cache-creation tokens.                                   |
| `estimated_cost_usd`         | REAL    | `0`     | Running USD cost total accumulated via `IncrementSessionCost`.      |

**`chat_cost_archive` table:**

When a session is deleted, `DeleteSession` archives its cost columns into this
table before removing the `chat_sessions` row. The transcript and title are NOT
preserved. `AggregateCost` queries `UNION ALL` over both `chat_sessions` and
`chat_cost_archive` so deleted sessions continue to contribute to the 30-day
dashboard chat-cost rollup.

| Column                | Type    | Default | Meaning                                       |
| --------------------- | ------- | ------- | --------------------------------------------- |
| `id`                  | TEXT PK | —       | Same session ID as the deleted `chat_sessions` row. |
| `project`             | TEXT    | —       | Project slug at deletion time.                |
| `model`               | TEXT    | `''`    | Model ID at deletion time.                    |
| `last_active`         | INTEGER | —       | Unix epoch of last activity; indexed for range queries. |
| `prompt_tokens`       | INTEGER | `0`     | Cumulative input tokens.                      |
| `completion_tokens`   | INTEGER | `0`     | Cumulative output tokens.                     |
| `cache_read_tokens`   | INTEGER | `0`     | Cumulative cache-read tokens.                 |
| `cache_creation_tokens` | INTEGER | `0`  | Cumulative cache-creation tokens.             |
| `estimated_cost_usd`  | REAL    | `0`     | Accumulated USD cost.                         |
| `deleted_at`          | INTEGER | —       | Unix epoch when the session was deleted.      |

Archive rows are retained indefinitely (each is ~80 bytes). There is no purge
mechanism.

**`estimated_cost_usd` precision:** stored as SQLite `REAL` (IEEE 754
double). The precision floor is approximately $0.0001 per frame; rounding
drift accumulates over long sessions. Dashboards round to two decimal places
for display. Exact sub-cent billing requires integer cents rather than `REAL`.
