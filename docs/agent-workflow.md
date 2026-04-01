# Phase 2: Agent Orchestration Architecture

This section describes the agreed design for how AI agents coordinate work
through ContextMatrix in Phase 2.

## Orchestration model

**Claude Code (CC) is the main agent.** There is no separate Go daemon
orchestrator for the primary workflow. The `Agent` tool built into CC handles
sub-agent spawning with clean contexts. P2.9 (Go orchestrator) is deferred — it
becomes a future headless/cron mode, not a Phase 2 core requirement.

```
Human ↔ CC (main agent)
           ├── Agent → sub-agent (execute-task)
           ├── Agent → sub-agent (execute-task)
           ├── Agent → sub-agent (execute-task)
           └── Agent → review agent (review-task)
```

All agents access ContextMatrix via MCP tools over HTTP (`POST /mcp`).

**Agents MUST always use MCP tools for all ContextMatrix interactions.** This
means `claim_card`, `heartbeat`, `update_card`, `complete_task`, etc. — never
`curl`, `wget`, or any direct REST API call. Direct HTTP is for human developers
verifying API handler code; it is not a supported interface for agent board
operations. This rule is enforced in the `workflowPreamble` injected into every
skill prompt and is explicitly stated in each skill file's Rules section.

## Skill files

Skill files are markdown documents in `skills/`. They serve two purposes:

1. **Human reference** — read directly from the repo
2. **MCP prompts** — served via `prompts/list` + `prompts/get` as Claude Code
   slash commands

The MCP server reads skill files from disk and serves them as named prompts. No
duplication — single source of truth.

When a slash command is invoked, the prompt handler returns a **delegation
wrapper** for most skills — not the raw skill content. The wrapper instructs
the receiving agent to call `get_skill(...)` to fetch the full instructions and
the required model, then spawn a sub-agent via the `Agent` tool with the
returned `model` and `content`. Skill files include an `## Agent Configuration`
section that specifies the model; this section is stripped from all content
delivered to agents (via `get_skill` and `complete_task`) since the model is
communicated as a separate `model` field.

Skills that use a two-phase sub-agent flow (currently only `create-plan`) can
specify a separate model for each phase in `## Agent Configuration`:

```markdown
## Agent Configuration

- **Model:** claude-opus-4-6 — Planning shapes everything downstream; worth the cost.
- **Phase 2 Model:** claude-haiku-4-5 — Subtask creation is mechanical; haiku is sufficient.
```

`get_skill` returns both models as separate fields: `model` (Phase 1) and
`phase2_model` (Phase 2, omitted when not present). Orchestrators must use
`phase2_model` when spawning the Phase 2 sub-agent so the correct (cheaper)
model is used for mechanical work like subtask creation.

**Exception — interview skills run inline:** `create-task` and `init-project`
require multi-turn conversations with the user, so their prompt handlers return
the **raw skill content** (with `## Agent Configuration` stripped) rather than
a delegation wrapper. These skills run directly in the main agent's context,
never as sub-agents. Delegating an interview skill to a sub-agent would break
the multi-turn flow because sub-agents cannot relay back-and-forth user messages
through the `Agent` tool.

```
skills/
  create-task.md    # /contextmatrix:create-task
  create-plan.md    # /contextmatrix:create-plan
  execute-task.md   # /contextmatrix:execute-task
  review-task.md    # /contextmatrix:review-task
  document-task.md  # /contextmatrix:document-task
  init-project.md   # /contextmatrix:init-project
```

## Slash command interface

CC exposes these slash commands via the MCP `prompts` capability:

| Command                        | Argument      | Type               | Description                       |
| ------------------------------ | ------------- | ------------------ | --------------------------------- |
| `/contextmatrix:create-task`   | `description` | optional free text | Start task creation interview     |
| `/contextmatrix:create-plan`   | `card_id`     | required           | Create plan + subtasks for a card |
| `/contextmatrix:execute-task`  | `card_id`     | required           | Claim and execute a task          |
| `/contextmatrix:review-task`   | `card_id`     | required           | Devils-advocate review of a task  |
| `/contextmatrix:document-task` | `card_id`     | required           | Write external docs for a task    |
| `/contextmatrix:init-project`  | `name`        | optional           | Initialize a new project board    |

Usage examples:

```
/contextmatrix:create-task I want to create a web page for my demo app
/contextmatrix:create-task there is a bug in the login form validation
/contextmatrix:create-plan ALPHA-001
/contextmatrix:execute-task ALPHA-003
/contextmatrix:review-task ALPHA-001
/contextmatrix:document-task ALPHA-001
```

For delegation-wrapper skills (`create-plan`, `execute-task`, `review-task`,
`document-task`), the server builds a delegation prompt instructing the
receiving agent to call `get_skill(...)` — which returns the full skill
instructions with injected card context, a `model` field, and optionally a
`phase2_model` field — then spawn a sub-agent via the `Agent` tool with the
returned `model`, `description` (short summary), and `prompt` (set to the
returned content). When `phase2_model` is present, use it for the Phase 2
sub-agent (e.g., subtask creation in `create-plan`). For inline skills
(`create-task`, `init-project`), the server returns raw skill content directly;
no sub-agent is involved.

## Workflow

**1. Task creation** (`/contextmatrix:create-task <description>`)

The prompt handler returns raw skill content (not a delegation wrapper). Main
agent (CC) runs the interview inline — gathering details from the human,
creating the card on the board, and offering next steps — all without spawning
a sub-agent. This is required because the interview needs multi-turn back-and-forth
with the user, which only works in the main agent's context.

**2. Planning** (`/contextmatrix:create-plan <card_id>`)

The slash command returns a **two-phase delegation prompt** instead of the
generic delegation wrapper used by other skills. This two-phase design prevents
sub-agent death during the user-approval wait — a sub-agent that waits for human
input can be killed by Claude Code before the user responds, leaving the
workflow stranded.

The flow is:

1. **Phase 1 — Plan drafting** (model: `claude-opus-4-6`): CC spawns a
   short-lived plan sub-agent that drafts the plan, writes it to the parent
   card body via `update_card`, and returns a `PLAN_DRAFTED` structured output
   immediately — without asking the user or waiting for approval. Opus is used
   here because planning quality affects all downstream work.
2. **User approval (CC handles directly)**: CC reads the card body, presents the
   `## Plan` section to the user, and asks for approval. CC is always alive for
   this — no sub-agent needed.
3. **Phase 2 — Subtask creation** (model: `claude-haiku-4-5`): Once the user
   approves, CC spawns a second short-lived sub-agent that reads the approved
   plan from the card body and creates all subtasks linked via the `parent`
   field. Haiku is used here because subtask creation is mechanical — reading a
   plan and calling `create_card` in a loop requires no reasoning depth.

Each phase uses a fresh sub-agent with a clean context and a short expected
lifetime. Neither phase waits for the user, so neither is vulnerable to
sub-agent timeout during an idle approval wait.

**3. Execution** (`/contextmatrix:execute-task <card_id>`)

CC spawns sub-agents in parallel (one per ready subtask). Each sub-agent:

1. Calls `get_task_context(id)` — reads everything before touching anything
2. Calls `claim_card(id, agent_id)`
3. Writes `## Plan` to card body, calls `update_card`
4. Works through the task, updating `## Progress` in card body as it goes
5. Calls `heartbeat` after every significant unit of work (mandatory)
6. Calls `complete_task(id, agent_id, summary)` when done
7. Prints structured completion summary (see below)

Main agent awaits all `Agent` tool completions and checks for blockers. **Parent
card state is managed automatically by the service layer:** when the first
subtask is claimed, the parent transitions `todo → in_progress`; when all
subtasks reach `done`, the parent transitions to `review`. Execute-task
sub-agents ignore any `next_step` field returned by `complete_task` — they
print `TASK_COMPLETE` and stop. The orchestrator is responsible for detecting
that the parent entered `review` and spawning the review sub-agent.

**4. Review** (`/contextmatrix:review-task <card_id>`)

Uses a two-phase flow to avoid sub-agent death during user-approval waits:

- **Phase 1 — Review sub-agent**: CC spawns a short-lived review sub-agent that
  evaluates the work, writes a `## Review Findings` section to the parent card
  body via `update_card`, releases its claim, and prints `REVIEW_FINDINGS`
  immediately — without asking the user or waiting for a decision.
- **User decision (CC handles directly)**: CC reads the card body, presents the
  `## Review Findings` section to the user, and asks for approve/reject. CC is
  always alive for this — no sub-agent needed.
- Based on the user's response, CC (the orchestrator) prints one of:
  - `REVIEW_APPROVED` — main agent proceeds to documentation (step 5).
  - `REVIEW_REJECTED` — main agent handles the rejection loop:
    1. Calls `transition_card` to move parent from `review` back to `in_progress`.
    2. Leaves existing `done` subtasks untouched — their work is preserved.
    3. Spawns a new planning sub-agent (create-plan) with the rejection feedback
       injected into the prompt, so it creates fix subtasks scoped to the issues.
    4. Resumes the execute → review cycle. This loop repeats until the human
       approves.

The parent card lifecycle with potential rejections:
`todo → in_progress → review → (rejected) in_progress → review → … → (approved) done`

**5. Documentation** (`/contextmatrix:document-task <card_id>`)

Uses a single-phase fire-and-report flow. CC spawns a short-lived documentation
sub-agent that reads the parent card + all subtasks and writes external
documentation (README updates, API docs, architecture notes) directly to disk —
no human approval gate before writing, since docs are generated from
already-reviewed, completed code. The sub-agent returns `DOCS_WRITTEN`
immediately with a list of files written. CC presents the summary to the user
and transitions the parent card to `done`.

## Board update ownership

- **Sub-agents** own their subtask: claim → write body throughout → complete
- **Main agent** owns parent task state transitions, user interactions, and
  approve/reject decisions
- **Review agent** evaluates the work, writes `## Review Findings` to the parent
  card body via `update_card`, releases its claim, and prints `REVIEW_FINDINGS`.
  It never asks the user for a decision — the orchestrator handles that after
  the sub-agent returns.
- **Documentation agent** writes documentation files only, never modifies cards.
  Returns `DOCS_WRITTEN` immediately — no human approval gate before writing.

## Sub-agent structured output

Sub-agents print a structured summary as their final output (`Agent` tool return
value). Main agent parses this to determine next steps.

On success:

```
TASK_COMPLETE
card_id: ALPHA-003
status: done
summary: Implemented JWT middleware, added tests, all passing
blockers: none
needs_human: false
```

On failure:

```
TASK_BLOCKED
card_id: ALPHA-003
status: blocked
reason: depends_on ALPHA-002 not yet done
blocker_cards: [ALPHA-002]
needs_human: false
```

```
TASK_BLOCKED
card_id: ALPHA-003
status: blocked
reason: Missing API credentials in config — cannot proceed
blocker_cards: []
needs_human: true
```

**`needs_human: false`** ONLY if every card in `blocker_cards` is currently in
`{in_progress, review, done}` — i.e., being worked by another agent in this same
execution batch. In all other cases, `needs_human: true`.

## Blocker recovery

Main agent logic when it receives `TASK_BLOCKED`:

```
if needs_human == false:
  verify all blocker_cards are in {in_progress, review, done}
  if yes → wait for siblings to complete, then re-spawn execute-task
  if no  → escalate to human (dep exists but nobody is working it)

if needs_human == true:
  pause all related tasks, surface to human, await instruction
```

Main agent uses `get_subtask_summary(parent_id)` to know when siblings have
finished before retrying.

## Card body structure

Sub-agents write and maintain this structure throughout execution:

```markdown
## Plan

Decided approach and rationale.

## Progress

- [x] Step 1: done, rationale
- [x] Step 2: done
- [ ] Step 3: in progress

## Notes

Gotchas, decisions made, alternatives rejected.
```

This is the durable audit trail. The structured stdout is ephemeral — the card
body is what persists in git history.

## Heartbeat discipline

Sub-agents **must** call `heartbeat` proactively after every significant unit of
work, before moving to the next step. The timeout checker (default 30min) will
mark a card `stalled` if heartbeat lapses. This is explicitly called out in
`execute-task.md` — it is not optional.

**Idle waits are the most common cause of stalled cards.** Any agent that holds
an active claim and waits for a sub-agent to complete must call `heartbeat`
every 5 minutes during that wait. This rule is enforced in the workflow preamble
injected into every skill prompt, and is explicitly called out in each skill
that has sub-agent-facing idle waits (`execute-task.md`). The main agent (CC)
never holds a card claim during user-facing waits — it handles those directly
between turns, making stalls in the main context impossible.

The two-phase design (used by `create-plan` and `review-task`) eliminates the
most common idle-wait failure mode entirely: sub-agents write their output to
the card body and return immediately; the always-alive orchestrator handles all
user interactions. The `document-task` skill uses the same principle — the doc
sub-agent writes to disk and returns `DOCS_WRITTEN` without waiting for
approval. No sub-agent in the current workflow idles for user input.

## Token cost configuration

Each skill step calls `report_usage` with the model that ran it so costs
accumulate on the parent card. Model rates are configured in `config.yaml`
under `token_costs` as cost-per-token values:

```yaml
token_costs:
  claude-haiku-4-5:  { prompt: 0.0000008,  completion: 0.000004  }  # $0.80 / $4.00 per MTok
  claude-sonnet-4-6: { prompt: 0.000003,   completion: 0.000015  }  # $3.00 / $15.00 per MTok
  claude-opus-4-6:   { prompt: 0.000005,   completion: 0.000025  }  # $5.00 / $25.00 per MTok
```

The `report_usage` call must pass `model` matching one of these keys. Costs for
the two planning phases differ deliberately: Phase 1 (plan drafting) runs on
`claude-opus-4-6` and Phase 2 (subtask creation) runs on `claude-haiku-4-5`,
reducing the cost of the mechanical subtask-creation pass significantly.

## Required permissions for target projects

Agents working on code repositories need the following Claude Code permissions
configured in the target project (e.g., `.claude/settings.local.json`):

**Claude Code tools:**
- `Edit` — modify existing files
- `Write` — create new files

**MCP tools (auto-available via MCP config):**
All `mcp__contextmatrix__*` tools are available once the MCP server is
configured. No per-tool allowlisting is needed for MCP tools.

**Bash tools (project-specific):**
- `Bash(go test:*)`, `Bash(make test:*)`, `Bash(make build:*)` etc. — vary by
  project language and build system

If `Edit` or `Write` is not in the target project's allowlist, execution agents
will report `TASK_BLOCKED` with an actionable error message explaining what
permissions are needed. The user must update the project's permissions config.

## Implementation order

The Phase 2 dependency chain for the agent orchestration workflow is:

```
P2.1 (MCP tools — extended set)
P2.2 (MCP transport + prompts) ◄── P2.1
P2.4 (dependency enforcement) ◄── P2.1
P2.10 (skill files) ◄── P2.2
P2.8 (token tracking) ◄── P2.1
P2.12 (dashboard) ◄── P2.8
P2.9 (orchestrator — headless mode only, deferred)
```
