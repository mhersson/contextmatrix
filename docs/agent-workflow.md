# Agent Orchestration Architecture

This document describes how AI agents coordinate work through ContextMatrix.

## Orchestration model

**An orchestrator agent drives each card.** It spawns sub-agents with clean
contexts via the `Agent` tool. The orchestrator is either a local
human-attached session (e.g. Claude Code, "CC" below) or the multi-model harness
that the agent backend runs inside a worker container. Two orchestration modes
exist:

1. **Interactive (HITL / local autonomous):** a local agent session (e.g. Claude
   Code) runs directly; the user triggers workflows via slash commands or the
   `run-autonomous` skill. Tasks with the `simple` label use a fast path that
   skips planning and review (see `docs/data-model.md` § Reserved labels).
2. **Agent backend:** `contextmatrix-agent` (a separate Go service) receives
   HMAC-signed webhooks from ContextMatrix and spawns disposable worker
   containers running the harness orchestrator. Autonomous cards start from
   `get_skill('run-autonomous', ...)` and run without a human channel; HITL
   cards start from `get_skill('create-plan', ...)` and keep the approval gates
   open via the chat channel. See `docs/remote-execution.md` for the backend
   architecture.

Board survey and card execution are separate sessions. Browsing the board to
decide what to work on (`list_cards`, `get_ready_tasks`) happens in its own
session; execution starts fresh with just a card ID - `start_workflow`
re-fetches everything the run needs. Everything a session reads becomes
permanent context re-billed on every later model call, so survey output must
not ride along into a run.

```text
Human ↔ CC (main agent, Opus)
           ├── Agent → sub-agent (plan-draft, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           └── inline: review-task (synthesizer = Opus, your session)
                  ├── Agent → specialist (correctness, size pick)
                  ├── Agent → specialist (design,      size pick)
                  └── Agent → specialist (security,    size pick)

Worker container → harness orchestrator (agent backend)
           ├── Agent → sub-agent (plan-draft)
           ├── Agent → sub-agent (execute-task)
           ├── Agent → sub-agent (execute-task)
           └── inline: review-task (synthesizer = orchestrator model, your session)
                  ├── Agent → specialist (correctness, size pick)
                  ├── Agent → specialist (design,      size pick)
                  └── Agent → specialist (security,    size pick)
```

The review skill runs inline in the orchestrator's session (so the `Agent` tool
is available to spawn the three parallel specialists); specialists run on the
review skill's size-based pick (`sonnet` default, `opus` for large change-sets)
regardless of the orchestrator's own model.

All agents access ContextMatrix via MCP tools over HTTP (`POST /mcp`).

**Agents MUST always use MCP tools for all ContextMatrix interactions.** This
means `claim_card`, `heartbeat`, `update_card`, `complete_task`, etc. - never
`curl`, `wget`, or any direct REST API call. Direct HTTP is for human developers
verifying API handler code; it is not a supported interface for agent board
operations. This rule is enforced in the `workflowPreamble` injected into every
skill prompt and is explicitly stated in each skill file's Rules section.

## Workflow Skill files

Skill files are markdown documents in `workflow-skills/`. They serve two
purposes:

1. **Human reference** - read directly from the repo
2. **MCP prompts** - served via `prompts/list` + `prompts/get` as Claude Code
   slash commands

The MCP server reads skill files from disk and serves them as named prompts. No
duplication - single source of truth.

When a slash command is invoked, the prompt handler returns a **delegation
wrapper** for most skills - not the raw skill content. The wrapper instructs the
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

**Why delegation wrappers exist:** Returning the full skill content directly to
the orchestrator agent lets it ignore model requirements, skip sub-agent
spawning, and bypass the ContextMatrix workflow (claim/heartbeat/complete) -
solving the underlying task while leaving orphaned cards across the board. The
delegation wrapper forces agents through the `get_skill` → `Agent` tool →
sub-agent pipeline, where lifecycle enforcement is structurally guaranteed
rather than relying on voluntary compliance. Any optimization to this flow must
preserve the forced indirection. The server-side inline execution mechanism (see
below) is the approved alternative: it still enforces lifecycle steps by wrapping
the content in a lifecycle-enforcing preamble before returning it.

**Exception - interview skills run inline:** `create-task` and `init-project`
require multi-turn conversations with the user, so their prompt handlers return
the **raw skill content** (with `## Agent Configuration` stripped) rather than a
delegation wrapper. These skills run directly in the main agent's context, never
as sub-agents. Delegating an interview skill to a sub-agent would break the
multi-turn flow because sub-agents cannot relay back-and-forth user messages
through the `Agent` tool.

**Server-side inline execution.** Two skills run inline, with different gating
rules:

- **`create-plan` and `brainstorming`** (model-matched inline): when the
  orchestrator passes its model family as `caller_model` to `get_skill` and it
  matches the skill's required model, the server returns the content wrapped in
  a lifecycle-enforcing inline preamble and sets `inline: true`. When the caller
  model doesn't match (or `caller_model` is absent), `inline` is `false` and
  behavior falls through to standard delegation (spawn a sub-agent on the
  required model). This saves the overhead of spawning a sub-agent on the same
  model the orchestrator is already running.

- **`review-task`** (always inline via `start_review`): the `start_review` MCP
  tool unconditionally returns `inline: true` for `review-task`, regardless of
  `caller_model`. The review skill spawns three specialist sub-agents in
  parallel via the `Agent` tool - and only the top-level (calling) session has
  the `Agent` tool; sub-agents spawned via `Agent` do not get `Agent`
  themselves. If the review ran in a spawned sub-agent it would silently degrade
  to a single-perspective walkthrough because the parallel spawn would not
  happen. The synthesizer runs on the orchestrator's own model (often Sonnet);
  the three specialists run on the model the review skill selects (`sonnet` by
  default, upgraded to `opus` for large change-sets). Do not reintroduce the
  model-match gate on `start_review` - it would reproduce the regression.
  (`get_skill('review-task')` still uses the model-match logic for any
  out-of-band callers; the workflow always goes through `start_review`.)

```
workflow-skills/
  create-task.md          # /contextmatrix:create-task (slash command + skill)
  init-project.md         # /contextmatrix:init-project (slash command + skill)
  create-plan.md          # skill only (loaded via get_skill / start_workflow)
  plan-draft.md           # skill only (spawned by create-plan plan drafting)
  execute-task.md         # skill only
  review-task.md          # skill only (loaded via start_review or get_skill)
  document-task.md        # skill only
  run-autonomous.md       # skill only (routed to by start_workflow when autonomous)
  brainstorming.md        # skill only
  systematic-debugging.md # skill only
                          # /contextmatrix:start-workflow (server-side only - no skill file)
```

Three slash commands exist: `create-task`, `init-project`, and `start-workflow`.
Phase-specific skills (`create-plan`, `plan-draft`, `execute-task`,
`review-task`, `document-task`, `run-autonomous`, `brainstorming`,
`systematic-debugging`) are loaded by the orchestrator via `get_skill` (or
`start_review` for the review-entry transition); they are not user-facing entry
points. This mirrors how `brainstorming` and `systematic-debugging` work.
`validSkillNames` in `internal/mcp/prompts.go` lists the complete set
addressable by `get_skill`.

`start-workflow` has no skill file. It exists as both a **prompt** (slash
command) and a **tool** (`start_workflow`). Both are server-side only: they
fetch the card, inspect the `autonomous` flag, and return the full skill content
for `run-autonomous` or `create-plan`. The tool enables natural-language
triggering - when a user writes "start workflow for ALPHA-001" (without a slash
command), the agent sees the `start_workflow` tool and calls it to get the
executable workflow content directly. If the card cannot be found, both paths
return an error.

## Task skills

In addition to the workflow skills served via MCP prompts (lifecycle
scaffolding), the active task backend mounts a set of operator-provided **task
skills** at `~/.claude/skills/` in the worker container. These are standard
Claude Code skills with `SKILL.md` files, discovered by the model via the native
Skill tool and engaged when their descriptions match the work being done. CM
ships no skills - operators point `task_skills.dir` at their own repo.

### Two-channel design

Workflow skills (existing): MCP prompts injected into the orchestrator's or a
sub-agent's first message. Drives lifecycle.

Task skills (new): Filesystem at `~/.claude/skills/<name>/SKILL.md`. Engaged
automatically by the model based on description matching.

The two channels never overlap: workflow skills tell the agent _what to do_,
task skills tell the agent _how to do it well_.

### Selection

A card's `skills` field constrains which task skills are mounted. See
`docs/data-model.md` for the field's three-state semantics. Resolution at
trigger time:

1. `card.skills` if set (including explicit empty);
2. else `project.default_skills` if set;
3. else mount the full set from `task_skills.dir` (the `dir` subfield of
   the `task_skills` config object).

### Engagement scoping

Workflow skills shape specialist-skill engagement through a
`## Specialist skills` section in the skill body. The section's prose tells the
agent whether and how to engage filesystem skills; it is multi-sentence
guidance, not a one-line marker. Engagement stays scoped to the right phase:

- `run-autonomous`: includes a `## Specialist skills` section instructing the
  orchestrator NOT to engage filesystem specialists; sub-agents will engage them
  during their work phase.
- `execute-task`, `review-task`, `document-task`: each includes a
  `## Specialist skills` section permitting engagement when descriptions match,
  requiring an `add_log(action="skill_engaged", ...)` on first engagement, and
  noting that workflow rules always take precedence over skill guidance.
- `create-plan`, `plan-draft`: no `## Specialist skills` section (create-plan
  orchestrates inline, plan-draft investigates and drafts only - no
  implementation work in either).
- `create-task`, `init-project`: no `## Specialist skills` section
  (interview/bootstrap, no implementation work).

### Backends

Both backends consume the per-card `task_skills` subset through the same
selection logic above. Each fetches a `{git_remote_url, ref}` pointer from CM -
the agent backend from `GET /api/agent/task-skills-source`, the chat backend
from `GET /api/chat/task-skills-source` - clones the repo server-side (CM mints
an instance-scoped clone token alongside the pointer), and read-only-mounts the
resolved subset at `~/.claude/skills/` in the worker container. A model-driven
Skill tool engages matching skills; engagement is reported via MCP
`add_log action=skill_engaged`, which drives the same `RecordSkillEngaged`
recording and dedup.

### Authoring convention

Task-skills are operator-provided - CM ships none. Point `task_skills.dir`
(optionally git-backed via `task_skills.git_remote_url`) at your own repo, laid
out as one directory per skill **at the repo root**. The directory name is the
skill name (referenced in `card.skills` and `project.default_skills`); flat, no
nesting:

```
go-development/SKILL.md
typescript-react/SKILL.md
...
```

Skills are engaged by description match, so anchor each `description` in
**observable activities and file types**, not subject areas - a topic-shaped
description ("Go programming guidance") engages too eagerly; a task-shaped one
("Use when implementing or modifying Go source files") engages on real work.

Each `SKILL.md` is YAML frontmatter + a markdown body:

```markdown
---
name: <skill-name-matching-dir-name>
description: Use when <observable activity>...
---

You are a <role>.

## When working on <activity>:

- Concrete pattern 1
- Concrete pattern 2
```

Optional frontmatter `allowed-tools: [Read, Write]` narrows the active tool set
when the skill is engaged (it never broadens). Push changes to your remote: the
agent and chat backends re-clone from the `{git_remote_url, ref}` pointer CM
derives on each run.

## Verification

A card's work is verified before it can pass review. The agent resolves the
verify command in this order:

1. **Declared** - the resolved `verify` command CM sends in the trigger payload
   (the card's `verify` merged over the project's; see `docs/data-model.md`). If
   present, the agent runs it as-is.
2. **Detected** - with no declared command, the agent detects the project's own
   command (test target, build script) from the repo.
3. **Model-proposed** - with nothing detected, the agent proposes a command.
4. **Loud skip** - if it cannot verify at all, it says so loudly rather than
   claiming success silently.

An operator can promote a model-proposed command into the project's `verify`
config (Project Settings → Verify) so future runs start from step 1. The
declared `timeout_seconds` and `env` bound and provision every step, not just the
declared command.

## Slash command interface

CC exposes these slash commands via the MCP `prompts` capability:

| Command                            | Argument         | Type               | Description                                                                                                                                                                                            |
| ---------------------------------- | ---------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `/contextmatrix:create-task`       | `description`    | optional free text | Start task creation interview                                                                                                                                                                          |
| `/contextmatrix:init-project`      | `name`           | optional           | Initialize a new project board                                                                                                                                                                         |
| `/contextmatrix:start-workflow`    | `card_id`        | required           | Drive a card through its full lifecycle, routed by the card's `autonomous` flag                                                                                                                        |

`/contextmatrix:start-workflow` is the canonical entry point: it inspects the
card's `autonomous` flag and routes to `run-autonomous` (autonomous cards) or
`create-plan` (HITL cards). Phase-specific prompts for `create-plan`,
`execute-task`, `review-task`, `document-task`, and `run-autonomous` are not on
the slash-command surface - they're internal orchestration steps, not user entry
points. The orchestrator loads each phase's skill via `get_skill` (or
`start_review` for the review-entry transition).

Usage examples:

```
/contextmatrix:create-task I want to create a web page for my demo app
/contextmatrix:create-task there is a bug in the login form validation
/contextmatrix:start-workflow ALPHA-001   # routes to run-autonomous or create-plan automatically
/contextmatrix:init-project my-new-project
```

The interview-style prompts (`create-task`, `init-project`)
return raw skill content for inline execution by the main agent - no sub-agent
involved. `start-workflow` returns the workflow skill (`create-plan` or
`run-autonomous`) wrapped in the inline-execution envelope; the orchestrator
runs it directly. Phase-specific skills loaded later via `get_skill` either run
inline (`create-plan` or `brainstorming` when caller_model matches the skill's
required model, `review-task` always) or are spawned as sub-agents via the
`Agent` tool with the returned `model`. The inline-eligible whitelist lives in
`inlineEligibleSkills` (`internal/mcp/prompts.go`): `review-task`,
`create-plan`, and `brainstorming`.

## Workflow

**1. Task creation** (`/contextmatrix:create-task <description>`)

The prompt handler returns raw skill content (not a delegation wrapper). Main
agent (CC) runs the interview inline - gathering details from the human,
creating the card on the board, and offering next steps - all without spawning a
sub-agent. This is required because the interview needs multi-turn
back-and-forth with the user, which only works in the main agent's context.

**2. Planning** (loaded internally - orchestrator calls
`get_skill('create-plan')`)

When a user invokes `/contextmatrix:start-workflow` on a HITL card (or the
`start_workflow` MCP tool routes there), the orchestrator drives planning -
delegating plan drafting to the plan-draft sub-agent - and creates subtasks
directly.

The flow is:

0. **Claim the card immediately**: The orchestrator calls `claim_card` as its
   very first action - before any exploration or planning begins. This moves the
   card to `in_progress` at the start of planning, not after subtasks are
   created. The card stays claimed through drafting, user approval, and subtask
   creation.
1. **Plan drafting (plan-draft sub-agent)**: The orchestrator fetches the
   plan-draft skill via `get_skill` and spawns it as a sub-agent. The sub-agent
   investigates the repository, writes `## Plan` and `## Decisions` to the
   parent card body via `update_card` - passing the orchestrator's `agent_id`,
   carried in a Board-write identity block in the spawn prompt, because the
   orchestrator holds the claim - and prints `PLAN_DRAFTED`. The orchestrator
   confirms the sections landed via `get_card`.
2. **User approval (orchestrator handles directly)**: The orchestrator presents
   the `## Plan` section to the user and asks for approval. No sub-agent needed.
3. **Subtask creation (inline)**: Once the user approves, the orchestrator
   re-reads the `## Plan` section via `get_card` - the plan lives on the card,
   not in its context - and creates all subtasks directly by calling
   `create_card` for each subtask. No sub-agent is spawned - this is trivial
   work that doesn't justify the overhead of a separate agent.

**3. Execution** (loaded internally - orchestrator calls
`get_skill('execute-task')`)

CC spawns sub-agents in parallel (one per ready subtask). Each sub-agent:

1. Calls `get_task_context(id)` - reads everything before touching anything
2. Calls `claim_card(id, agent_id)`
3. Writes `## Plan` to card body, calls `update_card`
4. Works through the task, updating `## Progress` in card body as it goes
5. Calls `heartbeat` after every significant unit of work (mandatory)
6. Calls `complete_task(id, agent_id, summary)` when done
7. Prints structured completion summary (see below)

Main agent awaits all `Agent` tool completions and checks for blockers. **Parent
card state is managed by the service layer and the orchestrator:** when the
first subtask is claimed, the parent transitions `todo → in_progress`. When all
subtasks reach `done`, the parent stays in `in_progress` - the orchestrator runs
documentation first, then manually transitions the parent to `review`.
Execute-task sub-agents ignore any `next_step` field returned by `complete_task` -
they print `TASK_COMPLETE` and stop.

The monitoring loop calls `await_subtasks` on the parent, passing its own
`agent_id` so the wait refreshes the orchestrator's claim on the parent while
it blocks - omitting `agent_id` makes the refresh a no-op. It blocks until
every subtask is terminal, one goes `stalled`, or the timeout passes. Each
return - completed, a stall to recover, or a timeout - is followed by
`report_usage` to
record the orchestrator's own token consumption against the parent card;
`heartbeat` is only needed on the completed return, since `await_subtasks`
already refreshed the claim on the others. The `model` field must be the
orchestrator's own model identifier (from its system context - "You are
powered by the model named X") - it must not be hardcoded. This is separate
from sub-agents' own `report_usage` calls; both are required.
After review completes, the orchestrator makes one final `report_usage` call to
capture remaining tokens before transitioning the parent to `done`.

**4. Documentation** (loaded internally - orchestrator calls
`get_skill('document-task')`)

Uses a single-phase fire-and-report flow. CC spawns a short-lived documentation
sub-agent that reads the parent card + all subtasks and writes external
documentation (README updates, API docs, architecture notes) directly to disk -
no human approval gate before writing. The sub-agent returns `DOCS_WRITTEN`
immediately with a list of files written. CC presents the summary to the user.
The parent card remains in `in_progress` during this phase.

**5. Review** (loaded internally via the `start_review` MCP tool)

The orchestrator calls `start_review(card_id, agent_id, caller_model)`, which
atomically transitions the parent to `review` AND returns the `review-task`
skill in one call - there is no path to load the review skill without committing
the transition. The response always has `inline: true`; the orchestrator runs
the skill in its own session (see "Server-side inline execution" above for why).
The flow:

- **Pass 1 - Spec compliance and test gate (synthesizer = orchestrator):** the
  orchestrator runs the project test suite and lint, plus a spec / scope check
  against the plan and acceptance criteria. If Pass 1 fails, it skips Pass 2
  entirely, writes findings with `recommendation: revise`, and prints
  `REVIEW_FINDINGS`. No specialists are spawned.
- **Pass 2 - Three parallel specialists:** if Pass 1 succeeds, the orchestrator
  spawns three `Agent` calls in a single message (`model` per the skill's
  size-based pick - `sonnet` default, `opus` for large change-sets -
  `subagent_type: general-purpose`): Correctness (bugs, edges, errors, races,
  test quality), Design & Maintainability (architecture, naming, complexity,
  docs), and Security & Performance (input validation, secrets, CVEs,
  complexity, leaks). Each specialist prompt carries the synthesizer's
  `agent_id` because `report_usage` and `add_log` enforce
  `agent_id == AssignedAgent` - specialists act on the synthesizer's behalf for
  board writes. Before returning, each specialist calls `report_usage` against
  the parent card with its own token consumption and model identifier, and its
  own `on_behalf_of` label (`specialist-correctness`, `specialist-design`,
  `specialist-security`) so its usage lands in its own bucket instead of
  merging into the synthesizer's; this is what makes the specialists' cost
  visible on the card as three distinct rows. Specialists do not claim,
  transition, or write findings to the card body - they return a structured
  Markdown report with severity-tiered findings.
- **Synthesis (synthesizer = orchestrator):** the orchestrator dedupes
  overlapping findings, applies the strictest-defensible severity, and decides
  the overall recommendation (any Critical → `revise`; Important without
  Critical → typically `revise` unless purely cosmetic; only Minor / none →
  `approve` or `approve_with_notes`). It writes the synthesized
  `## Review Findings` section to the parent card body via `update_card`, calls
  `report_usage` for the synthesizer work, and prints `REVIEW_FINDINGS`. The
  orchestrator does NOT release the claim - it keeps ownership for the next
  phase.
- **User decision (CC handles directly)**: CC reads the card body, presents the
  `## Review Findings` section to the user, and asks for approve/reject. No
  sub-agent - the orchestrator already holds the claim and is alive.
- Based on the user's response, the orchestrator prints one of:
  - `REVIEW_APPROVED` - proceeds to finalization (transitions parent to `done`).
  - `REVIEW_REJECTED` - the rejection loop:
    1. Calls `transition_card` to move parent from `review` back to
       `in_progress`.
    2. Leaves existing `done` subtasks untouched - their work is preserved.
    3. Re-spawns the plan-draft sub-agent with the review findings appended as
       revision feedback, so the revised `## Plan` contains only fix subtasks
       scoped to the issues.
    4. Resumes the execute → document → review cycle. This loop repeats until
       the human approves.

The parent card lifecycle with potential rejections:
`todo → in_progress → (docs) → review → (rejected) in_progress → (docs) → review → … → (approved) done`

## Autonomous mode

Cards with `autonomous: true` bypass human approval gates. The
`/contextmatrix:start-workflow` slash command (or the `start_workflow` MCP tool)
routes the card to `run-autonomous` automatically and drives the entire
lifecycle using the `run-autonomous.md` skill. The orchestrator model is set by
the invoker - the user's own model for local autonomous (typically Opus), the
agent backend's configured or per-card-selected model for a worker container.

## HITL chat surface

HITL runs and the global chat panel both expose a typed message channel back to
a live worker session. Two surfaces exist:

- **Per-card worker messages** -
  `POST /api/projects/{project}/cards/{id}/message` forwards human-typed content
  to a running worker container (`card.worker_status == "running"`). The
  endpoint is human-only (rejects non-human `X-Agent-ID`), bounds content to 8
  KB; the backend writes it to the worker's stdin as a user-typed stream-json
  frame and echoes it back through the worker-log stream so the UI shows it in
  the same transcript as the agent's output.
- **Global chat panel** - `/api/chats/*` drives `internal/chat.Manager`, which
  owns a SQLite-backed transcript store (sessions + messages), an idle-TTL
  reaper that warms sessions down to cold, an SSE hub for browser fan-out
  (`GET /api/chats/{id}/stream`), and a worker-log bridge that maps each worker
  log entry to a transcript row. Cold sessions rehydrate from the persisted
  transcript via `transcript.Build` before the container starts; the rehydration
  phase ends on the first user message or an explicit
  `chat_rehydration_complete` call. Orientation is the chat backend's own
  concern: the worker opens every epoch (cold open, resume, post-`/clear`)
  with an embedded primer that lives next to the environment it describes
  (`contextmatrix-chat`'s `internal/chatwork/primer.md`) - CM ships no
  orientation text and serves no `chat-mode` skill.

**Promote to autonomous from chat.** A human can promote a running HITL card
mid-flight: `POST /api/projects/{project}/cards/{id}/promote` (web UI) or the
`promote_to_autonomous` MCP tool (Claude). Both call
`service.PromoteToAutonomous` first (fail-closed: rejects terminal cards, flips
`autonomous: true`, appends an activity log entry, fires an SSE event); only on
success does the API endpoint fire the backend's `/promote` webhook so the
worker-side stdin message is written. Both surfaces gate on `human:` prefix -
agents cannot self-promote. The backend verifies the flag out-of-band via
`GET /api/v1/cards/{project}/{id}/autonomous` (HMAC-signed) before writing its
canned stdin message.

**Lifecycle phases (create-plan skill, HITL and autonomous):**

```
Phase 0:  Pre-planning Gate      → branch on card shape: maintenance-skip (Branch A), systematic-debugging sub-agent (Branch B, both modes), or brainstorming inline (Branch C, HITL only). Produces a ## Design or ## Diagnosis section before plan drafting.
Phase 1:  Plan Drafting          → spawns plan-draft sub-agent; it writes ## Plan + ## Decisions and emits PLAN_DRAFTED; orchestrator confirms via get_card
Phase 2:  Plan Approval Gate     → get_card autonomous check; HITL presents plan, autonomous skips
Phase 3:  Subtask Creation       → inline; re-reads ## Plan via get_card, dedupes, then create_card for each subtask
Phase 4:  Execution Gate         → get_card autonomous check; HITL asks to start, autonomous skips
Phase 5:  Execution              → checkout feature branch (branch_name); claim parent; get_ready_tasks; spawn execute-task sub-agents in parallel; aggregate worktree branches onto feature branch when worktree isolation used
Phase 6:  Documentation          → release claim, spawn document-task sub-agent, reclaim after DOCS_WRITTEN
Phase 7:  Review                 → transition to review, run review-task inline (always); orchestrator spawns 3 opus specialists in parallel and synthesizes findings
Phase 8:  Review Decision Gate   → get_card autonomous check; autonomous branches on recommendation, HITL asks
Phase 9:  Commit/Push/PR Gate    → get_card autonomous check; autonomous or worker-container HITL (CM_CARD_ID set) auto-commits/pushes/PR; local HITL asks
Phase 10: Finalization           → reclaim, report_usage, transition to done, release_card (mandatory)
```

For autonomous cards, `run-autonomous.md` drives the same lifecycle with these
phase labels. run-autonomous starts from the correct phase based on card state:

```
Step 0:  Claim the card        → claim_card called before any exploration begins
Step 1:  Create feature branch → if branch_name is set, git checkout -b <branch_name> (or checkout existing); skipped otherwise. Runs before planning or sub-agent spawning.
Phase 1: Plan Drafting         → orchestrator runs create-plan Phase 1 inline; drafting itself is spawned to the plan-draft sub-agent
Phase 2: Subtask Creation      → inline; orchestrator re-reads ## Plan via get_card, then calls create_card directly
Phase 3: Execution             → spawns execute-task sub-agents in parallel; cherry-picks worktree branches onto feature branch when worktree isolation used
Phase 4: Documentation         → spawns document-task sub-agent (parent in in_progress)
Phase 5: Review                → orchestrator transitions parent to review via start_review, runs review-task inline; spawns 3 opus specialists in parallel and synthesizes findings
Phase 6: Finalization          → transitions parent to done, final report_usage, release_card (mandatory)
```

The orchestrator claims the card and moves it to `in_progress` before
determining the starting phase. If the card is already `in_progress` or
`review`, the claim is still required - the starting-phase table determines
which phase to resume from.

**Guardrails:**

- **Branch protection** - agents must never push to `main` or `master`. The
  `report_push` tool returns a hard error if the branch name is `main` or
  `master`.
- **Maximum review cycles** - when a review returns `revise`, the orchestrator
  calls `increment_review_attempts` and then checks whether the returned count
  is `>= 3`; if so it calls `report_usage`, prints `AUTONOMOUS_HALTED`, and
  stops, requiring human intervention. The skill enforces an at-most-3 cap this
  way. The server applies a higher defense-in-depth cap (`maxReviewAttempts = 7`
  in `internal/service/service.go`) so a manual override can still proceed past
  3 if needed without bypassing the skill gate.
- **Await-based stall detection** - the orchestrator calls `await_subtasks` on
  the parent card, passing its own `agent_id` so the wait refreshes its claim
  while it blocks. The call blocks until every subtask is terminal, one goes
  `stalled`, or the timeout passes; a `stalled` return uses
  `check_agent_health` for per-card detail to respawn.
- **Human vetting gate** - cards imported from external sources (GitHub Issues,
  Jira, etc.) require explicit human approval before agents can work on them.
  `get_ready_tasks` automatically filters out unvetted external cards; a
  `claim_card` call on an unvetted card returns 403 `CARD_NOT_VETTED`. A human
  must inspect the card content in the web UI and enable the "Content vetted"
  toggle before any agent workflow can proceed. This prevents malicious
  instructions embedded in external issue bodies from being executed by agents.

Unlike the interactive workflow, the autonomous orchestrator skips user approval
between plan drafting and subtask creation. It only halts when review cycles are
exhausted or a sub-agent reports `needs_human: true`.

## Board update ownership

- **Sub-agents** own their subtask: claim → write body throughout → complete
- **Main agent** owns parent task state transitions, user interactions, and
  approve/reject decisions
- **Review agent** evaluates the work, writes `## Review Findings` to the parent
  card body via `update_card`, releases its claim, and prints `REVIEW_FINDINGS`.
  It never asks the user for a decision - the orchestrator handles that after
  the sub-agent returns.
- **Documentation agent** writes documentation files only, never modifies cards.
  Returns `DOCS_WRITTEN` immediately - no human approval gate before writing.

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
reason: Missing API credentials in config - cannot proceed
blocker_cards: []
needs_human: true
```

**`needs_human: false`** ONLY if every card in `blocker_cards` is currently in
`{in_progress, review, done}` - i.e., being worked by another agent in this same
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

Main agent calls `await_subtasks(parent_id)` to block until siblings finish
before retrying; `get_subtask_summary(parent_id)` is the point-in-time check
when a blocking wait is not wanted.

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

Parent cards accumulate sections per phase instead:

```markdown
## Design           <- brainstorming (HITL creative branch)
## Diagnosis        <- systematic-debugging sub-agent
## Plan             <- plan-draft sub-agent
## Decisions        <- plan-draft sub-agent
## Review Findings  <- review rounds; later rounds append as ## Review Findings (Round N)
```

`## Decisions` preserves the drafting context that would otherwise die with
the plan-draft sub-agent: `### Approach` (decided approach and why),
`### Rejected alternatives`, and `### Assumptions` (constraints discovered
during investigation). Empty subsections are omitted.

This is the durable audit trail. The structured stdout is ephemeral - the card
body is what persists in git history.

## Heartbeat discipline

Any owner-attributed card mutation - `update_card`, `add_log`,
`transition_card`, `report_usage`, `start_review`, `complete_task` - refreshes
`last_heartbeat` as part of the same write, at no extra cost: it piggybacks on
a persist and commit the mutation is already doing. An agent making steady
progress on a card never needs to call `heartbeat` explicitly. The timeout
checker (default 30min) marks a card `stalled` only when neither a mutation
nor an explicit heartbeat has landed within the window.

**Idle waits are the most common cause of stalled cards** - a wait produces no
mutation, so it earns no free heartbeat. Waits on sub-agents belong in
`await_subtasks` (below), which refreshes the claim on the parent for the
caller. Any agent that holds an active claim and waits some other way must call
`heartbeat` every 5 minutes during that wait. This rule is enforced in the workflow preamble
injected into every skill prompt, and is explicitly called out in each skill
that has sub-agent-facing idle waits (`execute-task.md`). User-facing waits
follow an edge-triggered pattern instead, because an orchestrator blocked on
human input gets no turns to heartbeat on: the skills call `heartbeat`
immediately before prompting (resetting the timeout clock, so waits shorter
than the timeout never stall) and again on resume. A wait longer than the
timeout still flips the claimed card to `stalled` - a transient, expected state
for a local Claude Code session parked at a gate - and the resume path recovers
it (`transition_card` to `in_progress` and `claim_card`, or via `todo` on
boards whose `transitions` do not allow `stalled` back to `in_progress`).
Remote worker runs never hit this: the agent backend heartbeats from a
background goroutine for the whole run, including human waits.

The fire-and-report design (used by `review-task` and `document-task`)
eliminates the most common idle-wait failure mode: sub-agents write their output
to the card body and return immediately; the always-alive orchestrator handles
all user interactions. `plan-draft` follows the same design - it writes the
plan to the card, returns the `PLAN_DRAFTED` marker, and never idles for user
input; revision feedback arrives via a fresh respawn. No sub-agent in the
current workflow idles for user input.

## Waiting for subtasks

`await_subtasks(project, parent_id, agent_id?, timeout_seconds?)` blocks
server-side until every subtask of `parent_id` is terminal (`done` or
`not_planned`), any subtask goes `stalled`, or the window expires. One call
replaces a sleep-and-check loop, whose real cost is not the sleep but the
orchestrator context re-read that every wake-and-check cycle drags along.

It always returns `{parent_id, completed, timed_out, counts, stalled,
waited_seconds}`, so a timeout is a checkpoint rather than an error: on
`timed_out: true`, call it again.

- **Early stall return** - one `stalled` subtask ends the wait immediately with
  `completed: false` and the offending IDs in `stalled`, so the orchestrator
  respawns it instead of sitting out the rest of the window. `stalled` is not
  terminal; `done` and `not_planned` are.
- **Claim refresh** - pass your own `agent_id` and the wait refreshes that
  agent's claim on the parent every 4 minutes, so the card you hold cannot
  stall while you block on it. An unclaimed parent, or one claimed by somebody
  else, is accepted silently: `agent_id` here identifies whose heartbeat to
  refresh, and is not a permission check.
- **Bounded window** - the server caps a single call at `await_max` (default
  8m); a larger `timeout_seconds` is clamped to it. Callers cannot raise the
  cap, only lower it.

The wait is event-driven: it subscribes to the board event bus before its first
check, so a transition landing between the check and the wait is still seen. A
30s re-list backs that up, since the bus drops events for subscribers that fall
behind rather than blocking publishers.

`get_subtask_summary` remains the right call for a one-shot count - it is the
same data without the block.

## Tool response shapes

Mutation and list tools return **card summaries**: every scalar and bounded
field, but never `body`, `activity_log`, or `usage_breakdown`. `heartbeat`
returns a minimal `{card_id, state, last_heartbeat}` ack. Only `get_card` and
`get_task_context` return full cards - they are the designated fetch tools,
and skills that need the body, the activity log, or the per-model usage
breakdown (resume, review diff-base, documentation, cost audit) call them
explicitly. Within `get_task_context`, the primary card and parent are full
while siblings are summaries - sibling detail is fetched per card via
`get_card`. The full table is in
[`docs/api-reference.md`](api-reference.md) under "Card payload shapes".

The rationale is context economics: tool results land in the calling agent's
context and are re-read on every subsequent model call, and card bodies grow
during a run. Echoing the body from a heartbeat or a log append multiplies
that cost for zero information gain.

`get_card` itself carries two further opt-ins for trimming what it returns:
`include_activity_log: false` drops the activity log (default `true` - the
log is often the larger half of a long-lived card's payload), and
`sections: ["Plan", ...]` returns only the named `## <heading>` body
sections instead of the full body (heading names without `##`; the
pseudo-entry `"intro"` includes the pre-heading text). Unlike the
skill-injection filter below, a `sections` request that matches nothing
returns an empty body rather than the full one - the caller asked for less
and must get less. Image attachment runs against the filtered body, so an
image referenced only by an omitted section is not attached. Full detail:
[`docs/api-reference.md`](api-reference.md) under "`get_card` payload
opt-ins".

The same economics apply to skill delivery. `get_skill`, `start_review`, and
`start_workflow` inject card context into the skill content; on the late-run
surfaces the injected body is filtered to the sections the skill consumes -
review-task gets the intro plus `## Plan`, `## Review Findings` (all rounds),
and `## Decisions`; document-task gets the intro plus `## Plan` and
`## Decisions`; the execute-task parent gets the intro plus `## Plan` - while
early-run skills (create-plan, plan-draft, brainstorming,
systematic-debugging, run-autonomous) and the execute-task subtask's own card
keep the full body. A bracketed note names any omitted sections and points at
`get_card`; when none of the filter's sections exist in a body, the full body
passes through unchanged. The table is in
[`docs/api-reference.md`](api-reference.md) under "Skill-injection body
filtering".

A caller that already holds the body - typically an orchestrator that just
called `get_card` - can skip re-injecting it entirely: pass
`include_card: false` to `get_skill`, `start_workflow`, or `start_review`.
This replaces the `### Body` block (and, for `execute-task`, the parent's
filtered body) with a pointer note back to `get_card`; the metadata header and
sibling briefs are unaffected. Defaults to `true` (the section filtering above
still applies on top of it) - `run-autonomous`'s fast path reads the body
directly, so leave it unset unless you are certain you already have the body.

The same economics separate sessions: survey the board
(`list_cards`, `get_ready_tasks`) in one session, then execute in a fresh
session seeded only with the card ID - survey output otherwise becomes
permanent context re-billed for the whole run.

## Token cost configuration

Each skill step - and the orchestrator itself - calls `report_usage` with the
model that ran it so costs accumulate on the parent card. Model rates are
configured in `config.yaml` under `token_costs` as cost-per-token values:

```yaml
token_costs:
  claude-haiku-4-5: { prompt: 0.000001, completion: 0.000005 } # $1.00 / $5.00 per MTok
  claude-sonnet-4-6: { prompt: 0.000003, completion: 0.000015 } # $3.00 / $15.00 per MTok
  claude-opus-4-6: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
  claude-opus-4-7: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
  claude-opus-4-8: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
```

The `report_usage` call must pass `model` matching one of these keys. The model
used depends on the orchestrator and phase - see the **Model Allocation**
section below for the full breakdown. `model` must be the model that actually
served the calls, read fresh from system context or usage frames - never
derived from the calling agent's name (an agent named
`claude-opus-5-orchestrator` is not proof the calls were served by
`claude-opus-5`; deriving the model that way prices the tokens against the
wrong rate row). The `recalculate_costs` tool reprices from the current rate
table: on cards with a usage breakdown every estimated bucket is re-priced
(stale prices corrected) while actual provider-reported costs are never
modified; on cards without a breakdown it only fills in costs for cards with
non-zero tokens but zero stored cost and never overwrites an existing cost.

Token counts are caller-reported in every mode - ContextMatrix never measures
tokens itself. `report_usage`'s `source` field (`"self"` default,
`"collector"`) records provenance as the bucket's sticky `counts_source`, the
counts-side counterpart of `cost_source`: `counts_source` marks buckets whose
counts came from a trusted collector reading real usage frames, `cost_source`
marks buckets whose cost came from the provider rather than the rate table.

`report_usage`'s `on_behalf_of` field overrides the bucket's `agent` key while
`agent_id` still has to satisfy the claim check. Any skill step that calls
`report_usage` using another identity's `agent_id` to satisfy that check (the
plan-draft and systematic-debugging sub-agents, and review's specialists - see
below) must pass its own `on_behalf_of` so its usage is attributed to itself
rather than merged into the claim holder's bucket.

## Model Allocation

Local (Claude Code) orchestration uses two models: **Opus** (strongest
reasoning) and **Sonnet** (cost-effective workhorse); Haiku is not used. The
agent backend instead runs an OpenRouter / OpenAI-gateway model as orchestrator
and selects per-task models by complexity tier (see the agent-backend table
below). In every case the orchestrator
decides whether each phase runs inline or as a sub-agent - the `inline` field
returned by `get_skill` (and by `start_review`, which loads the review-task
skill atomically with the state transition) uses exact model match, but the
orchestrator overrides it for phases where the decision is driven by context
management rather than model compatibility.

### HITL + Local Autonomous (Opus orchestrator)

| Phase            | Model  | Method                                               | Why                                                                                                             |
| ---------------- | ------ | ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Orchestrator     | Opus   | User's session (HITL) or run-autonomous (local auto) | Strongest reasoning for planning, review, and coordination                                                      |
| Planning         | Opus   | Sub-agent (plan-draft)                               | Decomposition and tier calibration are decision work; isolation pays the drafting tokens once, so Opus is affordable |
| Subtask creation | Opus   | Inline - calls `create_card()` directly              | Trivial work; spawning a sub-agent costs more in overhead than it saves                                         |
| Execution        | Sonnet | Sub-agent per subtask                                | Context isolation (fresh ~50K vs accumulated 150K+) and parallel execution; Sonnet is 1.67x cheaper at scale    |
| Review           | Opus   | Inline (start_review inline=true, Opus==Opus)        | Devil's advocate reasoning benefits from Opus; inline keeps findings in orchestrator context for human approval |
| Documentation    | Sonnet | Sub-agent                                            | Context isolation - orchestrator has 150K+ accumulated context by this phase; fresh sub-agent starts at ~25K    |

### Agent backend (worker container)

The agent backend has no fixed Opus/Sonnet split. CM sets the **orchestrator**
model per trigger (`backends.agent.default_model`, overridden by a card's model
pin); the harness's complexity selector picks the per-task **coder** and
**reviewer** models within the card's budget. The phase structure is the same as
local orchestration - only the concrete model per row differs. The selection
algorithm behind these tables is documented in `docs/model-selection.md`.

| Phase            | Model                         | Method                                        | Why                                                                            |
| ---------------- | ----------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------ |
| Orchestrator     | CM default or per-card pin     | Set at trigger time by CM                      | One orchestrator model drives planning, subtask creation, review synthesis, docs |
| Planning         | per `get_skill` (plan-draft, opus) | Sub-agent (plan-draft)                     | Decision work runs on the top tier; drafting exploration stays out of the orchestrator's permanent context |
| Subtask creation | orchestrator model             | Inline - calls `create_card()` directly        | Trivial work, no sub-agent needed                                              |
| Execution        | complexity-selected (coder)    | Sub-agent per subtask                          | Context isolation + parallel execution; selector matches model to task tier    |
| Review           | complexity-selected (reviewer) | Inline or sub-agent per `start_review`         | Stronger reviewer catches issues before costly rework loops                    |
| Documentation    | orchestrator model             | Sub-agent                                      | Context isolation - the worker container has no human to intervene if context grows too large |

### Inline/sub-agent decision model

The `inline` field returned by `get_skill` (and by `start_review`) uses **exact
model match** - it returns `true` when the caller's model family matches the
skill's model family AND the skill is on the inline-eligible whitelist
(`review-task`, `create-plan`, `brainstorming`):

- **Planning:** Always sub-agent (plan-draft) - drafting exploration stays out
  of the orchestrator's permanent context; the card body is the handoff.
- **Subtask creation:** Always inline - the orchestrator re-reads `## Plan` via
  `get_card` and calls `create_card` directly.
- **Execution, documentation:** Always sub-agent - orchestrator instructions
  specify this for context isolation and parallel execution. The inline field is
  not consulted.
- **Review:** Follow the inline field returned by `start_review` - this is the
  one phase where model compatibility matters. Opus caller gets `inline: true`
  (Opus==Opus) and runs review directly. Sonnet caller gets `inline: false`
  (Sonnet!=Opus) and spawns an Opus sub-agent. Either way, `start_review` has
  already transitioned the parent card to `review` before returning, so the
  state and the action are atomically tied.

### Why `run-autonomous.md` has no model

The orchestrator model is an operational concern, not a skill concern. Local
autonomous uses whatever model the user runs (typically Opus); the agent backend
sets the orchestrator model from `backends.agent.default_model` or the card's
model pin at trigger time. This separation lets the same skill file work for
both workflows without model override logic.

## Required permissions for target projects

Worker-container tool policy is the agent backend's concern, not a per-project
setting: the harness provisions a fixed internal tool set on every container -
file read/edit/write, search, an `Agent`/`Task` sub-agent tool in autonomous
runs, the `mcp__contextmatrix__*` board tools, and a bounded Bash surface. See
`docs/remote-execution.md` and the agent repo's harness tool policy for the
canonical list.

Local human-attached sessions (e.g. Claude Code) instead use your own tool
permissions (`.claude/settings.local.json`). An execution agent needs at least
`Edit` and `Write`; if either is missing it reports `TASK_BLOCKED` with an
actionable message, and you update the project's permissions config before
retrying.

