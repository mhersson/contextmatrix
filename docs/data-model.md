# Data Model & Domain Rules

Cards are markdown files with YAML frontmatter under
`<project>/tasks/<ID>.md` in the boards repo; `.board.yaml` holds the project
config. This document is the canonical reference for card parsing, the state
machine, validation and the Go types. Component wiring and the trust model
live in [architecture](architecture.md); the HTTP surface in the
[API reference](api-reference.md).

## Key domain rules

1. **Card IDs** are `PREFIX-NNN`, zero-padded to three digits and growing
   past three when needed (`ALPHA-001`, `ALPHA-1000`). The server allocates
   them from `next_id` in `.board.yaml`. Immutable.

2. **State transitions are enforced.** Transitions come from `.board.yaml`
   `transitions`; an invalid one returns 409 Conflict. `stalled` and
   `not_planned` are required built-in states (config validation rejects a
   project without them in `states` and `transitions`).
   - `stalled` is system-managed: any state may go to `stalled` on heartbeat
     timeout without listing it, but leaving `stalled` follows
     `transitions.stalled`. Entering `stalled` clears the claim.
   - `not_planned` follows normal rules: only states that list it can reach
     it, and nothing automatic ever moves a card there. It is terminal:
     entering it clears the claim, flushes deferred commits and removes the
     card from active-agent and open-task counts.
   - Terminal states (`done`, `not_planned`) are absorbing for the path
     walker (`FindShortestPath`, used by `TransitionTo` and therefore by
     `complete_task`): a card leaves `not_planned` only by a single explicit
     transition, and no path routes through a terminal state. Without this,
     completing a cancelled card would walk `not_planned` -> `todo` -> `done`.
     `not_planned` -> `todo` stays the way a human resumes a card.
   - `depends_on` gates `in_progress`: a transition, `PUT`, or `PATCH` into
     `in_progress` fails with `ErrDependenciesNotMet` (409) while any
     dependency is not `done`; `not_planned` does not satisfy a dependency.
     `get_ready_tasks` and the `dependencies_met` read-time field use the
     same rule; `blocked_by` lists the ids that fail it. The claim itself is not gated: `claim_card` takes the claim
     first and then runs the `todo` -> `in_progress` transition, so on a
     blocked card the claim stands, the card stays in `todo`, and the
     response carries `auto_transition_failed: true` with the reason. The
     REST claim endpoint never auto-transitions.

3. **One agent per card.** `POST .../cards/{id}/claim` returns 409 if the
   card is claimed. Only the assigned agent may mutate a claimed card:
   `X-Agent-ID` must match `assigned_agent` or the API returns 403. Unclaimed
   cards can be mutated by anyone. A terminal card cannot be claimed
   (`ErrCardTerminal`, 409) except by the agent already holding the claim: an
   agent keeps its claim through `done` until `ReleaseCard` flushes its
   deferred commits, so a re-claim there is a heartbeat refresh.

   On a shared board (`boards.shared: true`) ownership is the pair
   `(assigned_agent, claimed_via)`: the same agent ID claiming through
   another instance is refused with 409 while that instance's lease is live,
   and takes the card over with a higher `claim_epoch` once it has expired.
   `PUT`, `PATCH`, `DELETE`, `add_log` and `complete_task` check the pair. A
   claim with no `claimed_via` predates shared boards and is honoured by any
   instance. See [shared boards](shared-boards.md).

4. **Human identity.** Humans use agent IDs prefixed `human:` (`human:alice`).
   The claim system treats them like any agent. The web UI keeps its agent ID
   in localStorage and sends it as `X-Agent-ID`.

5. **Every mutation auto-commits**, with optional deferral. Commit messages
   are `[contextmatrix] CARD-ID: description` for system commits and
   `[agent:AGENT-ID] CARD-ID: description` for attributed ones. With
   `boards.git_deferred_commit: true` a claimed card's mutations are batched
   into one commit flushed on release, force-release, stall, a terminal
   worker status (`completed`, `failed`, `killed`), a `report_usage` on an
   unclaimed card, or entry into `review` or `not_planned`. Card creation and
   deletion, and any update, state change, log entry or parent
   auto-transition on a card that was unclaimed when the mutation started
   (REST or MCP alike), always commit immediately. At startup a non-shared
   auto-commit repo commits any leftover dirty paths as
   `[contextmatrix] recover uncommitted changes`.

6. **Activity log is append-only, capped at 50 entries.** Agents append via
   the `add_log` MCP tool; entries past 50 drop from the file (oldest first)
   but stay in git history. On a shared repo the merge resolver in
   `internal/boardmerge` also writes entries with `agent: system`, action
   `merge` and message `<rule>: <detail> (instance <id>)` for an overridden
   field, a re-minted card, a dangling reference an invariant repair dropped,
   and an invalid merge that fell back to the remote version. The rules
   `claim.epoch_wins`, `claim.terminal_over_stall`, `claim.double_claim` and
   `claim.active_over_release` record which side's claim tuple survived.

7. **Heartbeat timeout.** When `last_heartbeat` is older than
   `heartbeat_timeout` (default `30m`), `CardService.StartTimeoutChecker`
   (`internal/service/service_locks.go`) sets the card to `stalled`, clears
   the claim, commits and publishes `CardStalled`. `lock.Manager.FindStalled`
   only enumerates candidates and never mutates. The same sweep reaps
   abandoned parents: a parent in `in_progress`, unclaimed, untouched within
   the timeout and with no active subtask is stalled as well, since a parent
   is never itself claimed and would otherwise never time out.

   `last_heartbeat` is refreshed by the `heartbeat` MCP tool and by any
   owner-attributed mutation (`update_card`, `add_log`, `transition_card`,
   `report_usage`, `start_review`, `complete_task`) as part of that write.
   Only the owning agent's activity counts: a mutation by anyone else, or one
   with no attribution (REST `PUT` is system-attributed), never bumps it.
   REST `PATCH` passes `X-Agent-ID` through, so it bumps when the header names
   the owner.

   On a shared board a heartbeat is recorded in memory and the file's
   `last_heartbeat` (the lease) is rewritten only when older than
   `boards.lease_interval` (default `5m`); `lock.Manager.LastBeat` returns the
   newer of the two and every read path, the stall checker and the dashboard
   use it. The live beat counts only while the card carries this instance's
   claim. The stall checker acts on claims this instance granted and on
   claims with no `claimed_via`. A claim another instance granted is stalled
   only after its pushed lease has stayed unchanged for `boards.lease_timeout`
   (default `1h`) on the local clock, following a recent pull, through a
   push-verified sync cycle; the stall bumps `claim_epoch`. A claim the
   remote has not confirmed for `lease_timeout` is fenced: release,
   transition, state patch and worker-status writes return 403
   `AGENT_MISMATCH` until a sync cycle succeeds; heartbeats pass, and the
   stall checker skips the card. The fence lives in memory, so after a
   restart every own claim is fenced until the startup cycle succeeds.

8. **External source tracking.** Imported cards (Jira, GitHub Issues) record
   origin in `source`. `source.external_id` is indexed and queryable via
   `GET .../cards?external_id=PROJ-1234`, which makes imports idempotent.
   `source` is immutable after creation. `source.external_url` must use
   `http` or `https`; any other scheme is rejected at write time with 422
   (`ErrInvalidExternalURL`).

9. **Human vetting gate for imported cards.** Cards with a `source` carry
   `vetted`, `false` on creation. A non-human agent cannot claim an unvetted
   card (`ClaimCard` returns 403 `CARD_NOT_VETTED`); a human toggles
   `vetted: true` in the web UI first.
   - Internal cards (no `source`) always have `vetted: true`.
   - `vetted` is human-only: agents get 403 `HUMAN_ONLY_FIELD` via REST, and
     the MCP `update_card` tool does not expose it.
   - `get_ready_tasks` excludes unvetted external cards; `get_card` and
     `get_task_context` redact an unvetted card's title and body for
     non-human callers.
   - The web UI shows an "unvetted" badge and a warning banner.

10. **Parent auto-transitions.** When a subtask actually transitions to
    `in_progress` (via `UpdateCard`, `PatchCard` or the state machine) and
    its parent is in `todo`, the parent moves to `in_progress`. A claim alone
    does not trigger this: `Manager.Claim` never changes `state`. When all
    subtasks are `done` the parent stays `in_progress`; the orchestrator runs
    documentation, then moves the parent to `review`. `complete_task`
    reports when all siblings are done so the caller knows documentation can
    proceed.

11. **Subtask type is automatic and immutable.** Enforced on `CreateCard` and
    `UpdateCard` from the `parent` field:

    | Scenario                                           | Behaviour                                                                   |
    | -------------------------------------------------- | --------------------------------------------------------------------------- |
    | Created with a non-empty `parent`                  | `type` forced to `subtask`                                                  |
    | `UpdateCard` sets `parent` on a card that had none | `type` forced to `subtask`                                                  |
    | `UpdateCard` clears `parent`                       | a `subtask` type resets to the first entry in the project's `types`         |
    | `UpdateCard` keeps an existing `parent`            | `type` must stay `subtask`, else 422                                        |
    | No `parent` before or after                        | `type: subtask` rejected with 422                                           |

    `subtask` is built in and never listed in `.board.yaml` `types`. Do not
    pass `type` when setting or clearing `parent`.

12. **Duplicate subtask guard.** `CreateCard` with a `parent` looks for an
    existing subtask under that parent whose title matches (case-insensitive,
    trimmed) in a non-terminal state. On a match the existing card is
    returned; the handler still answers 201 with the card, so callers need no
    special case. This keeps an agent that re-enters subtask creation after a
    crash or context reset from orphaning duplicates.
    - The check runs under `writeMu`; no TOCTOU race.
    - `next_id` still increments; the gap is harmless.
    - A matching subtask that is `done` or `not_planned` does not block:
      re-doing finished work is intentional.

13. **Card deletion requires no subtasks.** `DELETE .../cards/{id}` removes
    the file (`os.Remove`) and enqueues a `CommitKindFile` commit; go-git
    records the deletion when the missing path is staged. It returns 422
    `VALIDATION_ERROR` if the card has subtasks, and 403 if the card is
    claimed by another agent. The web UI enables Delete only for `todo` or
    `not_planned` cards with no `assigned_agent`, behind a `ConfirmModal`.

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
claimed_via: laptop-3f9a2c   # granting instance (shared boards only)
claimed_at: 2026-03-30T14:00:00Z
claim_epoch: 4               # bumped on claim, release, stall, terminal move
parent: ""
subtasks: [ALPHA-003, ALPHA-004] # set via UpdateCard; not auto-populated
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
assignee: alice
autonomous: true
create_pr: true
branch_name: alpha-001/implement-user-auth
base_branch: develop
pr_url: https://github.com/org/repo/pull/42
review_attempts: 0
worker_status: ""
token_usage:
  model: claude-sonnet-4-6
  prompt_tokens: 12340
  completion_tokens: 5670
  cache_read_tokens: 80000     # omitted when zero
  cache_creation_tokens: 4000  # omitted when zero
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

A subtask looks the same except `type` is `subtask` and `parent` is set
(rule 11); it carries no `branch_name`:

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

The frontmatter is delimited by `---` lines; the body is free markdown.
Parsing splits on `---`: the first element is empty, the second YAML, the
third the body.

### `skills` (optional, `*[]string`)

Task-skill names mounted into the worker container's `~/.claude/skills/`.
Three states:

| Value                   | Meaning                                                                          |
| ----------------------- | -------------------------------------------------------------------------------- |
| absent (`nil`)          | inherit the project's `default_skills`, else the full set from `task_skills.dir` |
| `skills: []`            | no specialist skills for this card                                               |
| `skills: [name1, name2]` | only these are mounted                                                          |

A subtask created with `parent` set and no `skills` copies the parent's value
once at creation; later parent edits do not propagate. Settable via
`update_card`, REST `PATCH` (`skills_clear: true` resets to `nil`, since JSON
cannot express "absent" on a patch), hand-editing, or the per-card selector in
the card panel; project defaults via Project Settings. See
[agent workflow](agent-workflow.md) for the task-skill channel.

## Go type definitions

These match `internal/board/card.go` field for field.

```go
// internal/board/card.go

type Card struct {
    ID                      string          `yaml:"id"                              json:"id"`
    Title                   string          `yaml:"title"                           json:"title"`
    Project                 string          `yaml:"project"                         json:"project"`
    Type                    string          `yaml:"type"                            json:"type"`
    State                   string          `yaml:"state"                           json:"state"`
    Priority                string          `yaml:"priority"                        json:"priority"`
    AssignedAgent           string          `yaml:"assigned_agent,omitempty"        json:"assigned_agent,omitempty"`
    LastHeartbeat           *time.Time      `yaml:"last_heartbeat,omitempty"        json:"last_heartbeat,omitempty"`
    ClaimedVia              string          `yaml:"claimed_via,omitempty"           json:"claimed_via,omitempty"`
    ClaimedAt               *time.Time      `yaml:"claimed_at,omitempty"            json:"claimed_at,omitempty"`
    ClaimEpoch              int             `yaml:"claim_epoch,omitempty"           json:"claim_epoch,omitempty"`
    Parent                  string          `yaml:"parent,omitempty"                json:"parent,omitempty"`
    Subtasks                []string        `yaml:"subtasks,omitempty"              json:"subtasks,omitempty"`
    DependsOn               []string        `yaml:"depends_on,omitempty"            json:"depends_on,omitempty"`
    DependenciesMet         *bool           `yaml:"-"                               json:"dependencies_met,omitempty"`
    BlockedBy               []string        `yaml:"-"                               json:"blocked_by,omitempty"`
    Context                 []string        `yaml:"context,omitempty"               json:"context,omitempty"`
    Labels                  []string        `yaml:"labels,omitempty"                json:"labels,omitempty"`
    Skills                  *[]string       `yaml:"skills,omitempty"                json:"skills,omitempty"`
    Source                  *Source         `yaml:"source,omitempty"                json:"source,omitempty"`
    Custom                  map[string]any  `yaml:"custom,omitempty"                json:"custom,omitempty"`
    Assignee                string          `yaml:"assignee,omitempty"              json:"assignee,omitempty"`
    Autonomous              bool            `yaml:"autonomous,omitempty"            json:"autonomous"`
    ModelOrchestrator       string          `yaml:"model_orchestrator,omitempty"    json:"model_orchestrator,omitempty"`
    ModelCoder              string          `yaml:"model_coder,omitempty"           json:"model_coder,omitempty"`
    ModelReviewer           string          `yaml:"model_reviewer,omitempty"        json:"model_reviewer,omitempty"`
    BestOfN                 int             `yaml:"best_of_n,omitempty"             json:"best_of_n,omitempty"`
    MaxCapability           bool            `yaml:"max_capability,omitempty"        json:"max_capability,omitempty"`
    MobParticipants         int             `yaml:"mob_participants,omitempty"      json:"mob_participants,omitempty"`
    MobPhases               []string        `yaml:"mob_phases,omitempty"            json:"mob_phases,omitempty"`
    MobGuests               []string        `yaml:"mob_guests,omitempty"            json:"mob_guests,omitempty"`
    Verify                  *VerifyConfig   `yaml:"verify,omitempty"                json:"verify,omitempty"`
    Vetted                  bool            `yaml:"vetted,omitempty"                json:"vetted"`
    CreatePR                bool            `yaml:"create_pr,omitempty"             json:"create_pr,omitempty"`
    AwaitCI                 bool            `yaml:"await_ci,omitempty"              json:"await_ci,omitempty"`
    AwaitCopilotReview      bool            `yaml:"await_copilot_review,omitempty"  json:"await_copilot_review,omitempty"`
    BranchName              string          `yaml:"branch_name,omitempty"           json:"branch_name,omitempty"`
    BaseBranch              string          `yaml:"base_branch,omitempty"           json:"base_branch,omitempty"`
    PRUrl                   string          `yaml:"pr_url,omitempty"                json:"pr_url,omitempty"`
    ReviewAttempts          int             `yaml:"review_attempts,omitempty"       json:"review_attempts,omitempty"`
    WorkerStatus            string          `yaml:"worker_status,omitempty"         json:"worker_status,omitempty"`
    Phase                   string          `yaml:"phase,omitempty"                 json:"phase,omitempty"`
    TokenUsage              *TokenUsage     `yaml:"token_usage,omitempty"           json:"token_usage,omitempty"`
    UsageBreakdown          []UsageBucket   `yaml:"usage_breakdown,omitempty"       json:"usage_breakdown,omitempty"`
    SubtaskCostUSD          float64         `yaml:"-"                               json:"subtask_cost_usd,omitempty"`
    SubtaskCostHasEstimates bool            `yaml:"-"                               json:"subtask_cost_has_estimates,omitempty"`
    InPlaybooks             []string        `yaml:"-"                               json:"in_playbooks,omitempty"`
    Created                 time.Time       `yaml:"created"                         json:"created"`
    Updated                 time.Time       `yaml:"updated"                         json:"updated"`
    ActivityLog             []ActivityEntry `yaml:"activity_log,omitempty"          json:"activity_log,omitempty"`
    Body                    string          `yaml:"-"                               json:"body"`
}

type ActivityEntry struct {
    Agent     string    `yaml:"agent"           json:"agent"`
    Timestamp time.Time `yaml:"ts"              json:"ts"`
    Action    string    `yaml:"action"          json:"action"`
    Message   string    `yaml:"message"         json:"message"`
    Skill     string    `yaml:"skill,omitempty" json:"skill,omitempty"` // set on skill_engaged actions
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
    CountsSource        string  `yaml:"counts_source,omitempty"         json:"counts_source,omitempty"`
}
```

`Autonomous` and `Vetted` deliberately lack JSON `omitempty` so clients can
tell "explicitly false" from "not returned". `CreatePR` keeps `omitempty`: it
defaults at create time, so an absent key in an old file reads as false.

`CacheReadTokens` and `CacheCreationTokens` are absent when zero;
`RecalculateCosts` treats missing values as 0.

**Cost formula** (per `report_usage` call and in `RecalculateCosts`):

```text
estimated_cost_usd +=
    prompt_tokens         * rate.Prompt
  + cache_read_tokens     * (rate.CacheRead  || rate.Prompt * 0.10)
  + cache_creation_tokens * (rate.CacheWrite || rate.Prompt * 1.25)
  + completion_tokens     * rate.Completion
```

`rate.CacheRead` and `rate.CacheWrite` are explicit per-token rates; zero
falls back to the prompt-derived multiplier. The catalog supplies cache rates
when the gateway publishes `input_cache_read` / `input_cache_write` pricing
(OpenRouter does; plain OpenAI-protocol gateways usually do not). The single
1.25x fallback collapses the 5-minute and 1-hour cache-write tiers; agents
pass `cache_creation_input_tokens` from the stream-json `usage` frame as is.

### Usage breakdown

`UsageBreakdown` holds one `UsageBucket` per `(agent, model)` pair, merging
every `report_usage` call for that pair. It attributes cost after release
(when `assigned_agent` is cleared) and across several agents or models on
one card. Empty-agent buckets roll up to the dashboard's `unassigned` label.

- `cost_source` is `actual` when the cost came from the provider
  (`actual_cost_usd` on `report_usage`) or `estimated` when priced from the
  rate table. Actual is authoritative: `RecalculateCosts` re-prices only
  `estimated` buckets, and a bucket that ever received an actual cost stays
  `actual`.
- Token counts are caller-reported in every mode; CM never measures tokens.
  `counts_source` is `collector` when the counts came from a trusted collector
  reading real usage frames (`source: "collector"`), empty when
  self-reported (`source: "self"` or omitted). Sticky once `collector`.
- The bucket's `agent` key is `on_behalf_of` when passed, else `agent_id`.
  This lets the claim holder (whose `agent_id` must pass the ownership check)
  attribute a sub-agent's tokens under the sub-agent's name. `on_behalf_of`
  never affects authorization.
- The cumulative `TokenUsage` stays equal to the bucket sum: each report
  increments both. Cards with no buckets fall back to `assigned_agent` for
  their rollup.

```go
// internal/board/project.go

type Repo struct {
    Name    string `yaml:"name"              json:"name"`
    URL     string `yaml:"url"               json:"url"`
    Primary bool   `yaml:"primary,omitempty" json:"primary,omitempty"`
}

type RemoteExecutionConfig struct {
    WorkerImage     string `yaml:"worker_image,omitempty"      json:"worker_image,omitempty"`
    ChatWorkerImage string `yaml:"chat_worker_image,omitempty" json:"chat_worker_image,omitempty"`
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
    BoardsRepo       string                   `yaml:"-"                           json:"boards_repo,omitempty"`   // stamped on read by the composite store
    Repo             string                   `yaml:"repo,omitempty"              json:"repo,omitempty"`          // singular form; Repos takes precedence
    Repos            []Repo                   `yaml:"repos,omitempty"             json:"repos,omitempty"`         // multi-repo form; at most one Primary
    GitHubCredential string                   `yaml:"github_credential,omitempty" json:"github_credential,omitempty"`
    States           []string                 `yaml:"states"                      json:"states"`
    Types            []string                 `yaml:"types"                       json:"types"`
    Priorities       []string                 `yaml:"priorities"                  json:"priorities"`
    Transitions      map[string][]string      `yaml:"transitions"                 json:"transitions"`
    RemoteExecution  *RemoteExecutionConfig   `yaml:"remote_execution,omitempty"  json:"remote_execution,omitempty"`
    GitHub           *GitHubImportConfig      `yaml:"github,omitempty"            json:"github,omitempty"`
    DefaultSkills    *[]string                `yaml:"default_skills,omitempty"    json:"default_skills,omitempty"`
    Verify           *VerifyConfig            `yaml:"verify,omitempty"            json:"verify,omitempty"`
    Favorites        map[string]TierFavorites `yaml:"favorites,omitempty"         json:"-"`
    Templates        map[string]string        `yaml:"-"                           json:"templates,omitempty"`     // loaded from templates/ at runtime
}

// internal/board/verify.go

type VerifyConfig struct {
    Command        string   `yaml:"command,omitempty"         json:"command,omitempty"`
    TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
    Env            []string `yaml:"env,omitempty"             json:"env,omitempty"`
}
```

`ProjectConfig.EffectiveRepos()` normalises `repo` and `repos` into one
`[]Repo`: with `repos` set, empty names derive from the URL and the first
entry becomes `Primary` when none is marked; otherwise the singular `repo`
becomes `[]Repo{{URL: repo, Primary: true}}`. Validation rejects duplicate
names, empty URLs and more than one `Primary`.

`Favorites` holds per-project overrides for the agent backend's model
selector, keyed by complexity tier. Each `TierFavorites` is a bare list of
model slugs (every role) or a `{coder: [...], reviewer: [...]}` map; the
`favorites:` example under `backends.agent` in `config.yaml.example` shows
the same shape. `mergeFavorites` (`internal/api/backend_run.go`) combines the
global `backends.agent.favorites` with the project's at trigger time, project
entries winning per tier. `json:"-"`: no REST path writes it; hand-edit
`.board.yaml`. See [model selection](model-selection.md#the-decision-order).

**Immutable fields**: `id`, `project`, `created`, `source`; `branch_name`
after first generation.

**Server-managed fields**: `id`, `created`, `updated`, `assigned_agent`,
`last_heartbeat`, `claimed_via`, `claimed_at`, `claim_epoch`, `activity_log`,
`worker_status`, `review_attempts`, `branch_name`, `token_usage`,
`usage_breakdown`, `dependencies_met`, `blocked_by`, `subtask_cost_usd`,
`subtask_cost_has_estimates`, `in_playbooks`.

`dependencies_met`, `blocked_by`, `subtask_cost_usd`,
`subtask_cost_has_estimates` and `in_playbooks` are computed on read and
never written to frontmatter. `blocked_by` is the subset of `depends_on`
that is not `done`, in `depends_on` order; absent when every dependency is
met.
`subtask_cost_usd` sums the `estimated_cost_usd` of direct subtasks
(single-card GET only; omitted when zero) and `subtask_cost_has_estimates`
reports whether any of those costs include rate-table-estimated buckets.
`in_playbooks` lists the playbooks holding a card entry for the card (GET and
list); omitted when empty and best-effort, so a playbook-store failure leaves
it empty rather than failing the read.

**Agent-managed field** `phase`: the orchestrator's position within a run,
one of `plan`, `execute`, `judge`, `document`, `review`, `integrate`,
`pr_gates`, `done`; orthogonal to `state`. `judge` is used only by a Best-of-N
run to pick a winner before `document`. `pr_gates` waits on the PR's CI and
Copilot review when `await_ci` / `await_copilot_review` are set. Enum
validated; the empty string clears it. Settable via `update_card` and REST
`PUT` / `PATCH`.

**Section upsert**: the MCP `update_card` tool accepts
`upsert_section_heading` plus `upsert_section_content` (both or neither).
They replace-or-append one `## <heading>` block without resending the body: a
flush-left, case-sensitive `## <heading>` line is replaced in place, else the
section is appended. Resubmitting identical content leaves the body unchanged
but still advances `updated` and commits. Mutually exclusive with `body`
(`ErrInvalidSectionPatch`); the heading is one non-empty line with no leading
`#`; the result is checked against the 512 KB `maxBodyLen`. MCP-only,
mirrored by `service.PatchCardInput.UpsertSection`.

**Human-only fields** (REST callers whose `X-Agent-ID` starts with `human:`;
others get 403 `HUMAN_ONLY_FIELD`): `vetted`, `assignee`, `autonomous`,
`create_pr`, `await_ci`, `await_copilot_review`, `base_branch`, the model
pins (`model_orchestrator`, `model_coder`, `model_reviewer`), `best_of_n`,
`max_capability`, the mob fields, and `verify`. Where each is exposed:

| Field                             | POST | PUT | PATCH | MCP `update_card` |
| --------------------------------- | ---- | --- | ----- | ----------------- |
| `vetted`                          | yes  | yes | yes   | no                |
| `assignee`                        | yes  | yes | yes   | no                |
| `autonomous`                      | yes  | yes | yes   | yes (any agent)   |
| `create_pr`                       | yes  | yes | yes   | no                |
| `await_ci`, `await_copilot_review` | yes | yes | yes   | no                |
| `base_branch`                     | yes  | no  | yes   | no                |
| model pins                        | yes  | yes | yes   | no                |
| `best_of_n`                       | yes  | yes | yes   | no                |
| `max_capability`                  | yes  | yes | yes   | no                |
| mob fields                        | yes  | yes | yes   | no                |
| `verify`                          | yes  | no  | yes   | no                |

`PUT` is a full replace: omitting a field clears it (`base_branch` and
`verify`, absent from the PUT body, are preserved). `PATCH` leaves `nil`
fields unchanged. `create_pr` is nullable on POST: absent defaults in the
service layer (see [`create_pr`](#create_pr-semantics)), and an explicit
boolean from an agent is rejected. `best_of_n` is range-validated for every
caller to `0` (off) or `2..best_of_n.max_candidates`, else 400
`BAD_REQUEST`; it is sticky (no per-trigger override), acts only on the agent
backend, and is zeroed at trigger with a warning when the card's mob session
covers `execute` and the server allows checkpoints.

### `max_capability` (optional, bool)

Tells the agent backend to ignore cost when auto-selecting models: the most
capable candidate in the card's tier wins and favorites are bypassed. Copied
into the trigger payload (`TriggerPayload.MaxCapability`) in
`internal/api/backend_run.go`; the selection behaviour lives in the agent
repository. In the UI the "Maximum capability" checkbox appears only while
automatic model selection is on, and re-enabling automatic selection clears
pins but not this flag. See
[model selection](model-selection.md#the-decision-order).

### `autonomous` (optional, bool)

When `true` the card runs the autonomous lifecycle: `start_workflow` routes
to `run-autonomous`, human approval gates are bypassed, and the backend
forces `interactive` off. Two write paths:

- **MCP `update_card`** exposes `autonomous` (`*bool`) to any MCP-connected
  agent, so an agent harness can mark cards suitable for autonomous runs
  before any of them execute. `nil` leaves the value unchanged.
- **REST** keeps it human-only.

`promote_to_autonomous` (MCP, `agent_id` must start with `human:`) and
`POST .../cards/{id}/promote` flip an already running HITL card: idempotent,
409 on a terminal card, and the agent backend's `/promote` webhook calls this
endpoint first, fail-closed. Setting the flag via `update_card` only writes
it. See [agent workflow](agent-workflow.md) and
[running cards](running-cards.md).

### Mob fields (optional)

| Field              | Values                                 | Default |
| ------------------ | -------------------------------------- | ------- |
| `mob_participants` | 0 (off) or 2..`mob.max_participants`   | 0       |
| `mob_phases`       | duplicate-free subset of `plan`, `review`, `execute` | `[]` |
| `mob_guests`       | names from the `mob.guests` registry; requires `mob_participants >= 2` | `[]` |

With `mob_participants >= 2` an agent-backend run convenes that many internal
discussion seats in each listed phase; guests add operator-registered
external participants. Sticky like `best_of_n`. `execute` is accepted at
write time even while `mob.execute_checkpoints_enabled` is off; that gate
applies at trigger. PATCH validates the resulting card, so adding a guest is
checked against the stored participant count. Values are re-clamped against
the current config at trigger, and the trigger clamp is authoritative. Mob
coding takes priority over `best_of_n`. See
[remote execution](remote-execution.md#mob-sessions).

### `verify` (optional, `*VerifyConfig`)

An operator-declared verify gate, set on the project (`ProjectConfig.Verify`)
and optionally overridden per card. At trigger time CM resolves field by
field, card over project: `command` when non-empty, `timeout_seconds` when
`> 0`, `env` when non-nil.

`command` is one shell line run via `bash -c`; `timeout_seconds` bounds the
run (`0` = agent default) and applies to detected commands too; `env` lists
container environment variable names (never values) passed to the verify
subprocess and the model's bash tool. Values appear unredacted in transcripts
sent to the LLM provider, so never name a variable whose value embeds a
credential.

Both write paths validate (422 `VALIDATION_ERROR`) and normalise a zero-value
config to nil. Limits (`internal/service/verify.go`): `command` at most 1024
bytes, single line, no NUL; `timeout_seconds` in `0..7200`; `env` at most 16
names matching `[A-Z_][A-Z0-9_]*`, rejecting the prefixes `CM_`, `CMX_`,
`LLM_`, `GITHUB_` and the suffixes `_TOKEN`, `_KEY`, `_SECRET`, `_PASSWORD`.

### `assignee` (optional, string)

A bare username (`alice`, not `human:alice`) naming the responsible human.
Informational only: independent of `assigned_agent`, never read by claim,
release, stall or terminal transitions, never a permission boundary. Absent
from the MCP `create_card` / `update_card` tools (agents see it read-only on
`get_card`). Normalised (`TrimSpace` + lowercase) at the API boundary, capped
at 64 characters.

Validation forks on `auth.mode`:

- **`multi`:** a changed value must match a known, non-disabled username
  (case-insensitive) or be empty, else 422 `VALIDATION_ERROR`. An unchanged
  value always passes, so a user later removed from the roster, or a value
  hand-edited into the file, round-trips on unrelated edits.
- **`none`:** any change is rejected with 422 ("assignee requires multi-user
  mode"); an unchanged value round-trips.

Changing it (including clearing) appends an `assigned` activity entry
("Assigned to <user>" or "Unassigned (was <user>)") attributed to the acting
identity (empty normalises to `system`). `PATCH` on a card claimed by another
agent is 403 `AGENT_MISMATCH` regardless of fields. On `PUT`, omitting
`assignee` clears it.

The self-containment lint on MCP `create_card` / `update_card` appends a
`self_containment_warning` activity entry when the title or body matches a
local-path or foreign-repo signal; best-effort. See
[agent workflow](agent-workflow.md).

## Reserved labels

| Label    | Effect                                                                                                                                                                                                                                     |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `simple` | Autonomous fast path. With this label and no subtasks, `run-autonomous` skips planning, subtask creation, review and documentation, works directly and moves to `done`. Claims, heartbeats, tests and release still apply. Classified in `classifyComplexity()` (`internal/mcp/prompts.go`). |

## Card body templates

Templates live at `<project>/templates/<type>.md`; the filename without
`.md` must equal the card type. Plain markdown, no frontmatter. `LoadTemplates`
reads every `.md` file at startup and on project reload into
`ProjectConfig.Templates`, returned to agents via `get_task_context` and to
the UI as part of the project config.

In the create-card panel a template loads into the body when the type
changes and the body is not dirty; a dirty body asks for confirmation before
a template replaces it, and is left alone when the new type has no template.
Accepting a template clears the dirty flag.

## Playbooks

A playbook is a cross-project ordered list of steps: card references, manual
gate steps, or both. Playbooks are global team artifacts stored one file per
playbook at `<boards.dir>/playbooks/<id>.yaml`, at the top level of the boards
repo. Order is array order. Playbooks are not runnable: CM never executes
anything; a playbook is coordination state for humans and planning sessions.
See [playbooks](playbooks.md).

Each boards repo has its own `playbooks/` directory. IDs are unique across
repos (a create checks every repo before choosing a suffix), `boards_repo` on
create picks the repo, and every summary and detail carries `boards_repo`.

```yaml
id: alpha-rollout            # server-generated slug, immutable
title: Alpha feature rollout
description: optional free text shown on the detail page
created_by: human:alice      # informational attribution
created_at: 2026-08-20T09:00:00Z
updated_at: 2026-08-20T10:30:00Z
next_entry_id: 4             # persisted counter; entry IDs are never reused
entries:
  - id: e1
    type: card
    project: project-alpha
    card: ALPHA-101
    note: "merge this one first"
  - id: e2
    type: manual
    text: "Rebuild worker image and redeploy"
    done: true
    done_by: human:alice
    done_at: 2026-08-20T10:30:00Z
  - id: e3
    type: card
    project: project-beta
    card: BETA-042
```

### Rules

- **ID**: kebab-case slug derived from `title` at creation
  (`^[a-z0-9][a-z0-9-]*$`). Immutable: a title edit never renames the file.
  Collisions get a numeric suffix (`alpha-rollout-2`, ...) under the service
  write lock, so creation never surfaces a collision.
- **Entry IDs** are `e<N>` from the persisted `next_entry_id`; stable under
  reorder, never reused after deletion.
- **Card entries** (`type: card`) require `project` and `card` naming an
  existing card at add time. Duplicate `{project, card}` pairs in one
  playbook are 409 `PLAYBOOK_ENTRY_EXISTS`. Status is never stored; it is
  resolved live at read time. A deleted card or project leaves the entry as a
  broken reference (`missing: true`) that counts as incomplete.
- **Manual entries** (`type: manual`) require `text`, plus `done` / `done_by`
  / `done_at`. Checking `done` stamps `done_by` and `done_at` from the caller
  and the server clock; unchecking clears both; a re-check restamps.
- **`note`** is optional on both types and is a human-only channel: writable
  from the UI and MCP, excluded from agent-facing context.
- **Progress is derived**: a card entry is complete when its card is
  terminal (`done` or `not_planned`), a manual entry when `done`. Broken
  references count in the total, never in complete.
- **Empty `entries` is valid.**
- **Parsing is lenient**: unknown YAML fields are ignored, and a file that
  fails to parse or validate is skipped with a warning at load or reload. The
  store loads only non-dotfile `*.yaml` files.
- **Reserved name**: `playbooks` is a reserved top-level directory;
  `CreateProject` rejects it (case-insensitive). A project literally named
  `playbooks` in any repo disables the subsystem at startup (see
  [architecture](architecture.md#file-layout)).

### Go type definitions

```go
// internal/board/playbook.go

type Playbook struct {
    ID          string          `yaml:"id"                    json:"id"`
    Title       string          `yaml:"title"                 json:"title"`
    Description string          `yaml:"description,omitempty" json:"description,omitempty"`
    CreatedBy   string          `yaml:"created_by,omitempty"  json:"created_by,omitempty"`
    Created     time.Time       `yaml:"created_at"            json:"created_at"`
    Updated     time.Time       `yaml:"updated_at"            json:"updated_at"`
    NextEntryID int             `yaml:"next_entry_id"         json:"next_entry_id"`
    Entries     []PlaybookEntry `yaml:"entries"               json:"entries"`
}

type PlaybookEntry struct {
    ID      string     `yaml:"id"                json:"id"`
    Type    string     `yaml:"type"              json:"type"`
    Project string     `yaml:"project,omitempty" json:"project,omitempty"`
    Card    string     `yaml:"card,omitempty"    json:"card,omitempty"`
    Text    string     `yaml:"text,omitempty"    json:"text,omitempty"`
    Done    bool       `yaml:"done,omitempty"    json:"done,omitempty"`
    DoneBy  string     `yaml:"done_by,omitempty" json:"done_by,omitempty"`
    DoneAt  *time.Time `yaml:"done_at,omitempty" json:"done_at,omitempty"`
    Note    string     `yaml:"note,omitempty"    json:"note,omitempty"`
}
```

`Type` is `card` or `manual`. The service layer exposes a resolved view
(`PlaybookDetail` / `PlaybookEntryDetail` in `internal/service/playbooks.go`)
that joins card entries against the live card store; see the
[API reference](api-reference.md) for the JSON shape.

## Project board config format

```yaml
# boards/project-alpha/.board.yaml
name: project-alpha
display_name: "Project Alpha" # optional, shown in the UI
prefix: ALPHA
next_id: 1
repo: https://github.com/org/project-alpha.git
states: [todo, in_progress, blocked, review, done, stalled, not_planned]
types: [task, bug, feature] # "subtask" is built in; do not add it
priorities: [low, medium, high, critical]
transitions:
  todo: [in_progress, not_planned]
  in_progress: [blocked, review, todo]
  blocked: [in_progress, todo]
  review: [done, in_progress]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
# default_skills: [go-development, documentation]  # optional
```

`stalled` and `not_planned` must be present in `states` and `transitions`.
Any other state may be absent from `transitions`, which makes it terminal
(omit `done` for a truly final state; `done: [todo]` allows reopening). The
server injects `-> stalled` for every state. See [boards](boards.md) for
creating a board and templates.

**State names are part of the contract.** Besides the validator's
`stalled` / `not_planned` requirement, `todo`, `in_progress`, `review` and
`done` are hardcoded in MCP tools and service behaviour (`claim_card`
auto-transitions `todo -> in_progress`; `complete_task` moves subtasks to
`done` and parents to `review`; parent auto-transitions key off `todo` and
`in_progress`; dashboard metrics filter on `done`, `stalled`, `not_planned`).
Add states freely; do not rename the built-in six. See
[boards](boards.md#built-in-states) for each state's role.

Top-level `.board.yaml` fields (full reference in
[boards](boards.md#boardyaml-reference)):

| Field               | Meaning                                                                          |
| ------------------- | -------------------------------------------------------------------------------- |
| `name`              | Project slug; directory name and API key.                                        |
| `display_name`      | Optional UI label.                                                               |
| `prefix`            | Card ID prefix (`ALPHA` -> `ALPHA-001`).                                         |
| `next_id`           | Server-managed ID counter.                                                       |
| `repo`              | Code repository URL (single-repo form).                                          |
| `repos`             | Multi-repo list of `{name, url, primary}`; takes precedence over `repo`.         |
| `github_credential` | Credential-pool entry name (multi mode); see below.                              |
| `states`            | Workflow states; must include `stalled` and `not_planned`.                       |
| `types`             | Card types; `subtask` is built in.                                               |
| `priorities`        | Priority values.                                                                 |
| `transitions`       | State -> allowed target states; `stalled` is injected for every state.           |
| `remote_execution`  | `worker_image` / `chat_worker_image` overrides; see below.                       |
| `github`            | Issue import config; see below.                                                  |
| `default_skills`    | Project task-skill fallback; see below.                                          |
| `verify`            | Project verify gate; see the card-level `verify` field.                          |
| `favorites`         | Per-tier model preferences, hand-edited only; see the `Favorites` note.          |

### `default_skills` (optional, `*[]string`)

Project-wide fallback when a card has no `skills`; same three-state
semantics. A card's explicit `skills` (including `[]`) overrides it.

### `github_credential` (optional, `string`)

Name of an instance credential-pool entry used for the project's GitHub
operations: issue import, branch listing and the git token minted for a run's
worker. Boards-repo git always uses the instance credential. A reference
only; the token lives in the pool and is resolved server-side through
`TokenProviderFor`.
Empty means the instance-wide `github.*` credential, the only option in
`auth.mode: none` and the default for unbound projects in `multi`. Admin-only
to set. In `multi` an unknown name is 422 `VALIDATION_ERROR`; in `none` a
non-empty binding is rejected with 422 rather than silently falling back.

### `remote_execution` (optional, `*RemoteExecutionConfig`)

Whether cards may run remotely is instance-global (a configured task
backend), never per-project. `worker_image` feeds the task backend's card
runs only and `chat_worker_image` the chat backend only; neither falls back
to the other, since the two images bake different entrypoints. Empty means
that backend's own `base_image`. Both are validated against
`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$` with a 512-byte cap after trimming (422
`VALIDATION_ERROR` naming the field). On `PUT /api/projects/{project}` each
is merged by pointer: omitted preserves, empty string clears. See
[remote execution](remote-execution.md#worker-image-split).

### `github` (optional, `*GitHubImportConfig`)

Enables the per-project issue import loop when `import_issues` is true;
`owner` / `repo` name the source, `card_type`, `default_priority` and
`labels` shape the created cards. See
[GitHub issue import](github-issue-import.md).

### `verify` (optional) and `favorites` (optional)

Described under the card-level [`verify`](#verify-optional-verifyconfig)
field and the `Favorites` note above.

### `boards_repo` (API responses only)

Never in the file: the boards repository the project lives in, stamped on
read by the composite store (the first configured repo on a single-repo
instance).

## Server-side field-length limits

Enforced in the service layer; violations are 422 `VALIDATION_ERROR` with
`field` set. Constants live in `internal/service/service.go`.

| Field                     | Limit      | Constant                                                   |
| ------------------------- | ---------- | ---------------------------------------------------------- |
| `title`                   | 500 chars  | `maxTitleLen`                                              |
| `body`                    | 512 KB     | `maxBodyLen`; applies to the result of a section upsert too |
| individual label          | 100 chars  | `maxLabelLen`                                              |
| `labels` length           | 50 entries | `maxLabels`                                                |
| `depends_on` length       | 50 entries | `maxDependsOn`                                             |
| `agent_id` / `X-Agent-ID` | 256 chars  | `maxAgentIDLen`                                            |
| `assignee`                | 64 chars   | `maxAssigneeLen`                                           |
| `activity_log[].message`  | 2000 chars | `maxLogMessage`                                            |
| `activity_log[].action`   | 200 chars  | `maxLogAction`                                             |

Activity entries past the per-card cap of 50 (`MaxActivityLogEntries`) are
dropped oldest-first on append, not rejected.

## `worker_status` enum

`Card.WorkerStatus` tracks the worker independently of the workflow state.
Valid values (`validWorkerStatuses` in `internal/board/validation.go`):

| Value        | Set by                  | Meaning                                                                   |
| ------------ | ----------------------- | ------------------------------------------------------------------------- |
| `""`         | service layer / human   | No worker attached. Default on new cards; only a `completed` callback clears the stored value back to it. Card state transitions never touch it. |
| `queued`     | run trigger             | A run was requested; the container has not started.                       |
| `running`    | backend status callback | The worker is executing.                                                  |
| `failed`     | backend status callback | The worker exited with an error, or the trigger webhook failed.           |
| `killed`     | stop / stop-all         | The worker was stopped by a server-initiated stop.                        |
| `completed`  | backend status callback | The worker finished.                                                      |
| `parked`     | MCP `report_parked`     | The run parked the card for a human (review or PR gates); reason in the activity log. |

The backend reports through `POST /api/agent/status`, whose accepted subset
(`validWorkerCallbackStatuses`) is `running`, `failed`, `completed`; it
cannot set `queued`, `killed` or `parked`. An invalid value is 422
`VALIDATION_ERROR`. `parked` is set by the claiming agent right before its
container exits; the `completed` callback that follows preserves it while
clearing the claim, and the next trigger's `queued` replaces it.

## `depends_on` cycle detection

`UpdateCard` and `PatchCard` reject a change that would introduce a cycle.
After applying the requested `depends_on`, `detectDependencyCycle` walks the
graph from the card and reports any back-edge as a `ValidationError` wrapping
`ErrDependenciesNotMet` with `field: "depends_on"` and a message of the form
`circular dependency detected: ALPHA-001 and ALPHA-007 depend on each other`.
The check runs under `writeMu`.

`depends_on` is settable on `POST`, `PUT`, `PATCH`, `create_card` and
`update_card`. On `PATCH` and `update_card`, omitting it leaves the list
unchanged and `[]` clears it, the same nil/empty convention as `labels`.
Reference and cycle checks run against the resulting card, so an existing
`depends_on` is re-validated even when a patch touches other fields.

## `create_pr` semantics

Standalone and parent cards get a feature branch: `branch_name` is generated
at create as `<lowercase-id>/<title-slug>` and is immutable afterward.
Subtasks work on their parent's branch and get no `branch_name`. `create_pr`
decides only whether the run opens a pull request after pushing. When absent
on create it defaults to `true` for standalone and parent cards and `false`
for subtasks (the PR decision belongs to the parent). An explicit
`create_pr: false` pushes without a PR. Run and promote triggers never modify
the stored value.

## PR gates (`await_ci`, `await_copilot_review`)

Both are human-only booleans (no create-time defaulting) that matter only
when the run opens a PR. They gate the agent's `review -> done` transition
inside the `pr_gates` phase: `await_ci` keeps the card in `review` until the
PR's checks pass, and `await_copilot_review` has the agent request a Copilot
review and address valid findings first. The flags have no server-side
coupling to `create_pr`. Round limits, parking and unavailability handling
are agent behaviour, described in [running cards](running-cards.md).

## `chat_sessions` SQLite schema

Chat state lives in the operational store `ops.db`, separate from the boards
repo and `images.db`; the same file holds `model_blacklist` and
`model_outcomes`. `ensureSchema` in `internal/opstore/sqlite/schema.go` runs
`CREATE TABLE IF NOT EXISTS` for every table in its final shape: no
migration ledger and no compatibility path, so schema changes are edits to
that DDL. `chat_messages` carries `kind TEXT NOT NULL DEFAULT ''` (used for
the clear-context divider) and a unique index on `(session_id, seq)`.

**`chat_sessions`:**

| Column                      | Type    | Default | Meaning                                                        |
| --------------------------- | ------- | ------- | -------------------------------------------------------------- |
| `id`                        | TEXT PK | -       | ULID-shaped session identifier.                                |
| `title`                     | TEXT    | -       | Session name (auto-filled from the first user message).        |
| `project`                   | TEXT    | -       | Associated project; empty for cross-project sessions.          |
| `status`                    | TEXT    | -       | `cold`, `active`, `warm-idle` or `ending`; indexed.            |
| `created_at`                | INTEGER | -       | Unix epoch of creation.                                        |
| `last_active`               | INTEGER | -       | Unix epoch of last activity; indexed for range queries.        |
| `created_by`                | TEXT    | -       | Agent ID of the creator (owner in multi mode).                 |
| `container_id`              | TEXT    | NULL    | Worker container ID; cleared when the session goes cold.       |
| `workspace`                 | TEXT    | NULL    | JSON-encoded workspace directory list.                         |
| `model`                     | TEXT    | `''`    | Orchestrator model ID.                                         |
| `context_tokens`            | INTEGER | `0`     | Last context-window token count.                               |
| `context_tokens_updated_at` | INTEGER | NULL    | Unix epoch of the last context-token update.                   |
| `rehydration_active`        | INTEGER | `0`     | Boolean flag for the rehydration phase.                        |
| `rehydration_started_at`    | INTEGER | NULL    | Unix epoch when rehydration started.                           |
| `prompt_tokens`             | INTEGER | `0`     | Cumulative input tokens.                                       |
| `completion_tokens`         | INTEGER | `0`     | Cumulative output tokens.                                      |
| `cache_read_tokens`         | INTEGER | `0`     | Cumulative cache-read tokens.                                  |
| `cache_creation_tokens`     | INTEGER | `0`     | Cumulative cache-creation tokens.                              |
| `estimated_cost_usd`        | REAL    | `0`     | Running USD total, accumulated via `IncrementSessionCost`.     |

**`chat_cost_archive`:** `DeleteSession` copies the cost columns here before
removing the session row; transcript and title are not kept. `AggregateCost`
unions both tables so deleted sessions still count in the 30-day dashboard
rollup. Rows are retained indefinitely; there is no purge.

| Column                  | Type    | Default | Meaning                                    |
| ----------------------- | ------- | ------- | ------------------------------------------ |
| `id`                    | TEXT PK | -       | The deleted session's ID.                  |
| `project`               | TEXT    | -       | Project at deletion time.                  |
| `model`                 | TEXT    | `''`    | Model ID at deletion time.                 |
| `last_active`           | INTEGER | -       | Unix epoch of last activity; indexed.      |
| `prompt_tokens`         | INTEGER | `0`     | Cumulative input tokens.                   |
| `completion_tokens`     | INTEGER | `0`     | Cumulative output tokens.                  |
| `cache_read_tokens`     | INTEGER | `0`     | Cumulative cache-read tokens.              |
| `cache_creation_tokens` | INTEGER | `0`     | Cumulative cache-creation tokens.          |
| `estimated_cost_usd`    | REAL    | `0`     | Accumulated USD cost.                      |
| `deleted_at`            | INTEGER | -       | Unix epoch of deletion.                    |

`estimated_cost_usd` is a SQLite `REAL` (IEEE 754 double); rounding drift
accumulates over long sessions and dashboards round to two decimals. Exact
sub-cent billing would need integer cents.
