# Agent Orchestration Architecture

This document describes how AI agents coordinate work through ContextMatrix.

## Orchestration model

**Claude Code (CC) is the main agent.** The `Agent` tool built into CC handles
sub-agent spawning with clean contexts. Two orchestration modes exist:

1. **Interactive (HITL / local autonomous):** CC runs directly, user triggers
   workflows via slash commands or the `run-autonomous` skill. Tasks with the
   `simple` label use a fast path that skips planning and review (see
   `docs/data-model.md` § Reserved labels).
2. **Remote runner:** `contextmatrix-runner` (a separate Go binary) receives
   HMAC-signed webhooks from ContextMatrix and spawns disposable Docker
   containers running CC with the `run-autonomous` skill. See
   `docs/remote-execution.md` for the runner architecture.

```text
Human ↔ CC (main agent, Opus)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           └── Agent → review agent (review-task, Opus inline)

Runner container → CC (orchestrator, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           └── Agent → sub-agent (review-task, Opus)
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
wrapper** for most skills — not the raw skill content. The wrapper instructs the
receiving agent to call `get_skill(...)` to fetch the full instructions and the
required model, then spawn a sub-agent via the `Agent` tool with the returned
`model` and `content`. Skill files include an `## Agent Configuration` section
that specifies the model; this section is stripped from all content delivered to
agents (via `get_skill` and `complete_task`) since the model is communicated as
a separate `model` field.

Each skill specifies its model in `## Agent Configuration`. The `get_skill` tool
returns the model alongside the skill content. The orchestrator decides whether
to run inline or spawn a sub-agent based on the phase (see the **Model
Allocation** section below for the full decision model).

**Why delegation wrappers exist:** An earlier design returned the full skill
content directly to the orchestrator agent. In practice, agents ignored model
requirements, skipped sub-agent spawning, ignored the ContextMatrix workflow
(claim/heartbeat/complete), and just solved the underlying task — leaving
orphaned cards across the board. The delegation wrapper pattern was introduced
specifically to force agents through the `get_skill` → `Agent` tool → sub-agent
pipeline, where lifecycle enforcement is structurally guaranteed rather than
relying on voluntary compliance. Any optimization to this flow must preserve the
forced indirection. The server-side inline execution mechanism (see below) is
the approved alternative: it still enforces lifecycle steps by wrapping the
content in a lifecycle-enforcing preamble before returning it.

**Exception — interview skills run inline:** `create-task` and `init-project`
require multi-turn conversations with the user, so their prompt handlers return
the **raw skill content** (with `## Agent Configuration` stripped) rather than a
delegation wrapper. These skills run directly in the main agent's context, never
as sub-agents. Delegating an interview skill to a sub-agent would break the
multi-turn flow because sub-agents cannot relay back-and-forth user messages
through the `Agent` tool.

**Server-side inline execution for model-matched skills:** `review-task` and
`create-plan` support **inline execution** when the orchestrator's model matches
the skill's required model. This is controlled by the `get_skill` tool: when the
orchestrator passes its model family as `caller_model` and it matches the skill
model, `get_skill` returns the content wrapped in a lifecycle-enforcing inline
preamble and sets `inline: true` in the response. The delegation wrapper
instructs the orchestrator to execute inline when `inline` is true, or delegate
as usual when false. This saves the overhead of spawning a sub-agent on the same
model the orchestrator is already running. When `caller_model` is absent,
`inline` is always false and behavior is identical to the standard delegation
flow.

```
skills/
  create-task.md      # /contextmatrix:create-task
  create-plan.md      # /contextmatrix:create-plan
  execute-task.md     # /contextmatrix:execute-task
  review-task.md      # /contextmatrix:review-task
  document-task.md    # /contextmatrix:document-task
  init-project.md     # /contextmatrix:init-project
  run-autonomous.md   # /contextmatrix:run-autonomous
                      # /contextmatrix:start-workflow  (server-side only — no skill file)
```

`start-workflow` has no skill file. It exists as both a **prompt** (slash
command) and a **tool** (`start_workflow`). Both are server-side only: they fetch
the card, inspect the `autonomous` flag, and return the full skill content for
`run-autonomous` or `create-plan`. The tool enables natural-language triggering
— when a user writes "start workflow for ALPHA-001" (without a slash command),
the agent sees the `start_workflow` tool and calls it to get the executable
workflow content directly. If the card cannot be found, both paths return an
error.

## Slash command interface

CC exposes these slash commands via the MCP `prompts` capability:

| Command                          | Argument      | Type               | Description                                                        |
| -------------------------------- | ------------- | ------------------ | ------------------------------------------------------------------ |
| `/contextmatrix:create-task`     | `description` | optional free text | Start task creation interview                                      |
| `/contextmatrix:create-plan`     | `card_id`     | required           | Create plan + subtasks for a card                                  |
| `/contextmatrix:execute-task`    | `card_id`     | required           | Claim and execute a task                                           |
| `/contextmatrix:review-task`     | `card_id`     | required           | Devils-advocate review of a task                                   |
| `/contextmatrix:document-task`   | `card_id`     | required           | Write external docs for a task                                     |
| `/contextmatrix:init-project`    | `name`        | optional           | Initialize a new project board                                     |
| `/contextmatrix:run-autonomous`  | `card_id`     | required           | Run full autonomous lifecycle for a card                           |
| `/contextmatrix:start-workflow`  | `card_id`     | required           | Start the workflow for a card, routing automatically based on card |

`/contextmatrix:start-workflow` is a convenience entry point: it inspects the
card's `autonomous` flag and routes to `run-autonomous` (autonomous cards) or
`create-plan` (HITL cards). Use it when you know the card ID but not which
workflow applies.

Usage examples:

```
/contextmatrix:create-task I want to create a web page for my demo app
/contextmatrix:create-task there is a bug in the login form validation
/contextmatrix:create-plan ALPHA-001
/contextmatrix:execute-task ALPHA-003
/contextmatrix:review-task ALPHA-001
/contextmatrix:document-task ALPHA-001
/contextmatrix:start-workflow ALPHA-001   # routes to run-autonomous or create-plan automatically
```

For delegation-wrapper skills (`create-plan`, `execute-task`, `review-task`,
`document-task`), the server builds a delegation prompt instructing the
receiving agent to call `get_skill(...)` — which returns the full skill
instructions with injected card context and a `model` field — then spawn a
sub-agent via the `Agent` tool with the returned `model`, `description` (short
summary), and `prompt` (set to the returned content). For inline skills
(`create-task`, `init-project`), the server returns raw skill content directly;
no sub-agent is involved.

## Workflow

**1. Task creation** (`/contextmatrix:create-task <description>`)

The prompt handler returns raw skill content (not a delegation wrapper). Main
agent (CC) runs the interview inline — gathering details from the human,
creating the card on the board, and offering next steps — all without spawning a
sub-agent. This is required because the interview needs multi-turn
back-and-forth with the user, which only works in the main agent's context.

**2. Planning** (`/contextmatrix:create-plan <card_id>`)

The slash command returns a delegation prompt that instructs the orchestrator to
run planning inline and create subtasks directly.

The flow is:

1. **Plan drafting (inline)**: The orchestrator runs the create-plan skill
   inline — no sub-agent. It drafts the plan, writes it to the parent card body
   via `update_card`, and produces `PLAN_DRAFTED` structured output. Running
   inline retains the plan context for subtask creation.
2. **User approval (orchestrator handles directly)**: The orchestrator presents
   the `## Plan` section to the user and asks for approval. No sub-agent needed.
3. **Subtask creation (inline)**: Once the user approves, the orchestrator
   creates all subtasks directly by calling `create_card` for each subtask in
   the plan. No sub-agent is spawned — this is trivial work that doesn't justify
   the overhead of a separate agent.

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
card state is managed by the service layer and the orchestrator:** when the first
subtask is claimed, the parent transitions `todo → in_progress`. When all
subtasks reach `done`, the parent stays in `in_progress` — the orchestrator runs
documentation first, then manually transitions the parent to `review`.
Execute-task sub-agents ignore any `next_step` field returned by `complete_task`
— they print `TASK_COMPLETE` and stop.

During the monitoring loop the orchestrator (CC) calls `heartbeat` on the parent
card every 5 minutes and immediately follows each heartbeat with `report_usage`
to record the orchestrator's own token consumption against the parent card. The
`model` field must be the orchestrator's own model identifier (from its system
context — "You are powered by the model named X") — it must not be hardcoded.
This is separate from sub-agents' own `report_usage` calls; both are required.
After review completes, the orchestrator makes one final `report_usage` call to
capture remaining tokens before transitioning the parent to `done`.

**4. Documentation** (`/contextmatrix:document-task <card_id>`)

Uses a single-phase fire-and-report flow. CC spawns a short-lived documentation
sub-agent that reads the parent card + all subtasks and writes external
documentation (README updates, API docs, architecture notes) directly to disk —
no human approval gate before writing. The sub-agent returns `DOCS_WRITTEN`
immediately with a list of files written. CC presents the summary to the user.
The parent card remains in `in_progress` during this phase.

**5. Review** (`/contextmatrix:review-task <card_id>`)

The orchestrator transitions the parent to `review` before spawning the review
agent. Uses a two-phase flow to avoid sub-agent death during user-approval
waits:

- **Phase 1 — Review sub-agent**: CC spawns a short-lived review sub-agent that
  evaluates both the code and any documentation written in step 4, writes a
  `## Review Findings` section to the parent card body via `update_card`,
  releases its claim, and prints `REVIEW_FINDINGS` immediately — without asking
  the user or waiting for a decision.
- **User decision (CC handles directly)**: CC reads the card body, presents the
  `## Review Findings` section to the user, and asks for approve/reject. CC is
  always alive for this — no sub-agent needed.
- Based on the user's response, CC (the orchestrator) prints one of:
  - `REVIEW_APPROVED` — main agent proceeds to finalization (transitions parent
    to `done`).
  - `REVIEW_REJECTED` — main agent handles the rejection loop:
    1. Calls `transition_card` to move parent from `review` back to
       `in_progress`.
    2. Leaves existing `done` subtasks untouched — their work is preserved.
    3. Spawns a new planning sub-agent (create-plan) with the rejection feedback
       injected into the prompt, so it creates fix subtasks scoped to the
       issues.
    4. Resumes the execute → document → review cycle. This loop repeats until
       the human approves.

The parent card lifecycle with potential rejections:
`todo → in_progress → (docs) → review → (rejected) in_progress → (docs) → review → … → (approved) done`

## Autonomous mode

Cards with `autonomous: true` bypass human approval gates. The
`/contextmatrix:run-autonomous` slash command drives the entire lifecycle for a
single card using the `run-autonomous.md` skill. The orchestrator model is set
by the invoker — Opus for local autonomous (user's session), Sonnet for the
remote runner (via container config).

**Lifecycle phases:**

```
Phase 1: Plan Drafting      → inline, calls create-plan skill
Phase 2: Subtask Creation   → inline, orchestrator calls create_card directly
Phase 3: Execution          → spawns execute-task sub-agents in parallel
Phase 4: Documentation      → spawns document-task sub-agent (parent in in_progress)
Phase 5: Review             → orchestrator transitions parent to review, follows inline field
Phase 6: Finalization       → transitions parent to done
```

The orchestrator determines the starting phase from the card's current state and
body content (e.g., if the card is already `review`, it starts at Phase 5).

**Guardrails:**

- **Branch protection** — agents must never push to `main` or `master`. The
  `report_push` tool returns a hard error if the branch name is `main` or
  `master`.
- **Maximum review cycles** — the orchestrator checks the card's
  `review_attempts` field before each review cycle. After 2 failed reviews it
  prints `AUTONOMOUS_HALTED` and requires human intervention.
- **Heartbeat-based stall detection** — the orchestrator calls `heartbeat` on
  the parent card every 5 minutes and uses `check_agent_health` to detect and
  respawn stalled sub-agents.

Unlike the interactive workflow, the autonomous orchestrator skips user approval
between plan drafting and subtask creation. It only halts when review cycles are
exhausted or a sub-agent reports `needs_human: true`.

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

The fire-and-report design (used by `review-task` and `document-task`)
eliminates the most common idle-wait failure mode: sub-agents write their output
to the card body and return immediately; the always-alive orchestrator handles
all user interactions. `create-plan` avoids the problem entirely by running
inline on the orchestrator — no sub-agent is spawned. No sub-agent in the
current workflow idles for user input.

## Token cost configuration

Each skill step — and the orchestrator itself — calls `report_usage` with the
model that ran it so costs accumulate on the parent card. Model rates are
configured in `config.yaml` under `token_costs` as cost-per-token values:

```yaml
token_costs:
  claude-haiku-4-5: { prompt: 0.0000008, completion: 0.000004 } # $0.80 / $4.00 per MTok
  claude-sonnet-4-6: { prompt: 0.000003, completion: 0.000015 } # $3.00 / $15.00 per MTok
  claude-opus-4-6: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
```

The `report_usage` call must pass `model` matching one of these keys. The model
used depends on the orchestrator and phase — see the **Model Allocation**
section below for the full breakdown.

## Model Allocation

The system uses two models: **Opus** (strongest reasoning) and **Sonnet**
(cost-effective workhorse). Haiku is not used in any workflow. The orchestrator
decides whether each phase runs inline or as a sub-agent — the `inline` field
from `get_skill` uses exact model match, but the orchestrator overrides it for
phases where the decision is driven by context management rather than model
compatibility.

### HITL + Local Autonomous (Opus orchestrator)

| Phase            | Model  | Method                                               | Why                                                                                                             |
| ---------------- | ------ | ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Orchestrator     | Opus   | User's session (HITL) or run-autonomous (local auto) | Strongest reasoning for planning, review, and coordination                                                      |
| Planning         | Opus   | Inline on orchestrator                               | Orchestrator already is Opus — no spawn needed, retains plan context for subtask creation                       |
| Subtask creation | Opus   | Inline — calls `create_card()` directly              | Trivial work; spawning a sub-agent costs more in overhead than it saves                                         |
| Execution        | Sonnet | Sub-agent per subtask                                | Context isolation (fresh ~50K vs accumulated 150K+) and parallel execution; Sonnet is 1.67x cheaper at scale    |
| Review           | Opus   | Inline (get_skill inline=true, Opus==Opus)           | Devil's advocate reasoning benefits from Opus; inline keeps findings in orchestrator context for human approval |
| Documentation    | Sonnet | Sub-agent                                            | Context isolation — orchestrator has 150K+ accumulated context by this phase; fresh sub-agent starts at ~25K    |

### Remote Runner (Sonnet orchestrator)

| Phase            | Model  | Method                                                         | Why                                                                                |
| ---------------- | ------ | -------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Orchestrator     | Sonnet | Runner container sets model via `--model` / env var            | Cost control — Opus premium not justified for well-defined protocol                |
| Planning         | Sonnet | Inline on orchestrator                                         | Sonnet 4.6 plans well; inline avoids spawn overhead and retains plan context       |
| Subtask creation | Sonnet | Inline — calls `create_card()` directly                        | Same as HITL — trivial work, no sub-agent needed                                   |
| Execution        | Sonnet | Sub-agent per subtask                                          | Context isolation + parallel execution; same rationale as HITL                     |
| Review           | Opus   | Sub-agent (get_skill inline=false, Sonnet!=Opus → spawns Opus) | Only phase where Opus premium pays off — catches issues before costly rework loops |
| Documentation    | Sonnet | Sub-agent                                                      | Context isolation — runner has no human to intervene if context grows too large    |

### Inline/sub-agent decision model

The `inline` field from `get_skill` uses **exact model match** — it returns
`true` when the caller's model family matches the skill's model family:

- **Planning, subtask creation:** Always inline — orchestrator instructions
  override the inline field. The orchestrator retains context for downstream
  phases.
- **Execution, documentation:** Always sub-agent — orchestrator instructions
  specify this for context isolation and parallel execution. The inline field is
  not consulted.
- **Review:** Follow the `get_skill` inline field — this is the one phase where
  model compatibility matters. Opus caller gets `inline: true` (Opus==Opus) and
  runs review directly. Sonnet caller gets `inline: false` (Sonnet!=Opus) and
  spawns an Opus sub-agent.

### Why `run-autonomous.md` has no model

The orchestrator model is an operational concern, not a skill concern. Local
autonomous uses whatever model the user runs (typically Opus). The remote runner
sets Sonnet via container configuration (`--model` flag or environment
variable). This separation allows the same skill file to work for both workflows
without code duplication or model override logic.

## Required permissions for target projects

Agents working on code repositories need the following Claude Code permissions
configured in the target project (e.g., `.claude/settings.local.json`):

**Claude Code tools:**

- `Edit` — modify existing files
- `Write` — create new files

**MCP tools (auto-available via MCP config):** All `mcp__contextmatrix__*` tools
are available once the MCP server is configured. No per-tool allowlisting is
needed for MCP tools.

**Bash tools (project-specific):**

- `Bash(go test:*)`, `Bash(make test:*)`, `Bash(make build:*)` etc. — vary by
  project language and build system

If `Edit` or `Write` is not in the target project's allowlist, execution agents
will report `TASK_BLOCKED` with an actionable error message explaining what
permissions are needed. The user must update the project's permissions config.
