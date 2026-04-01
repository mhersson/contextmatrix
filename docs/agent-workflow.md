# Phase 2: Agent Orchestration Architecture

This section describes the agreed design for how AI agents coordinate work
through ContextMatrix in Phase 2.

## Orchestration model

**Claude Code (CC) is the main agent.** There is no separate Go daemon
orchestrator for the primary workflow. The `Task` tool built into CC handles
sub-agent spawning with clean contexts. P2.9 (Go orchestrator) is deferred — it
becomes a future headless/cron mode, not a Phase 2 core requirement.

```
Human ↔ CC (main agent)
           ├── Task tool → sub-agent (execute-task)
           ├── Task tool → sub-agent (execute-task)
           ├── Task tool → sub-agent (execute-task)
           └── Task tool → review agent (review-task)
```

All agents access ContextMatrix via MCP tools over HTTP (`POST /mcp`).

## Skill files

Skill files are markdown documents in `skills/`. They serve two purposes:

1. **Human reference** — read directly from the repo
2. **MCP prompts** — served via `prompts/list` + `prompts/get` as Claude Code
   slash commands

The MCP server reads skill files from disk and serves them as named prompts. No
duplication — single source of truth.

When a slash command is invoked, the prompt handler returns a **delegation
wrapper**, not the raw skill content. The wrapper instructs the receiving agent
to call `get_skill(...)` to fetch the full instructions and the required model,
then spawn a sub-agent via TaskCreate with the returned `model` and `content`.
Skill files include an `## Agent Configuration` section that specifies the
model; this section is stripped from all content delivered to agents (via
`get_skill` and `complete_task`) since the model is communicated as a separate
`model` field.

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

The server uses the arguments to build a delegation wrapper prompt. When the
receiving agent acts on a slash command, it calls `get_skill(...)` — which
returns the full skill instructions with injected card context and a `model`
field — then spawns a sub-agent via TaskCreate with the returned `model`,
`subject`, and `description` (set to the returned content).

## Workflow

**1. Task creation** (`/contextmatrix:create-task <description>`)

Main agent (CC) interviews the human to gather details, creates the card on the
board, then asks if the human wants a plan created immediately.

**2. Planning** (`/contextmatrix:create-plan <card_id>`)

The slash command returns a delegation prompt. CC calls
`get_skill(skill_name='create-plan', card_id=...)` to get the full prompt and
model, then spawns a plan sub-agent via TaskCreate (passing the `model` from
`get_skill`). The plan agent interviews the human, drafts a plan, and when
approved: updates the parent card body with the plan and creates all subtasks
linked via `parent` field.

**3. Execution** (`/contextmatrix:execute-task <card_id>`)

CC spawns sub-agents in parallel (one per ready subtask). Each sub-agent:

1. Calls `get_task_context(id)` — reads everything before touching anything
2. Calls `claim_card(id, agent_id)`
3. Writes `## Plan` to card body, calls `update_card`
4. Works through the task, updating `## Progress` in card body as it goes
5. Calls `heartbeat` after every significant unit of work (mandatory)
6. Calls `complete_task(id, agent_id, summary)` when done
7. Prints structured completion summary (see below)

Main agent awaits all Task tool completions and checks for blockers. **Parent
card state is managed automatically by the service layer:** when the first
subtask is claimed, the parent transitions `todo → in_progress`; when all
subtasks reach `done`, the parent transitions to `review`. The `complete_task`
tool returns embedded `review-task` skill content when this happens, so the main
agent can immediately spawn the review sub-agent.

**4. Review** (`/contextmatrix:review-task <card_id>`)

Spawned automatically by main agent when all subtasks are done. The review agent
reads the parent card + all subtasks, presents a devils-advocate assessment,
asks the human for an explicit approve/reject decision, and prints one of two
structured output blocks:

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

Spawned by main agent after review approval. The documentation agent reads the
parent card + all subtasks and writes external documentation (README updates,
API docs, architecture notes) as appropriate. Presents docs to human for
approval before writing. After docs are done, main agent transitions parent to
`done`.

## Board update ownership

- **Sub-agents** own their subtask: claim → write body throughout → complete
- **Main agent** owns parent task state transitions
- **Review agent** reads only during evaluation. Asks the human for an explicit
  approve/reject decision, then prints structured output (`REVIEW_APPROVED` /
  `REVIEW_REJECTED`) for the main agent to parse. Does not write to the board
  (only `claim_card`/`release_card`).
- **Documentation agent** writes documentation files only, never modifies cards

## Sub-agent structured output

Sub-agents print a structured summary as their final output (Task tool return
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
