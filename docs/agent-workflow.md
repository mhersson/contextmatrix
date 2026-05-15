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
   containers running CC. The runner supports two container modes: autonomous
   primes Claude with `get_skill('run-autonomous', ...)` and runs without a
   human channel; HITL (human-in-the-loop) primes Claude with
   `get_skill('create-plan', ...)` and keeps the human approval gates open via
   the chat channel. See `docs/remote-execution.md` for the runner architecture.

```text
Human ↔ CC (main agent, Opus)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           └── inline: review-task (synthesizer = Opus, your session)
                  ├── Agent → specialist (correctness, Opus 4-7)
                  ├── Agent → specialist (design,      Opus 4-7)
                  └── Agent → specialist (security,    Opus 4-7)

Runner container → CC (orchestrator, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           ├── Agent → sub-agent (execute-task, Sonnet)
           └── inline: review-task (synthesizer = Sonnet, your session)
                  ├── Agent → specialist (correctness, Opus 4-7)
                  ├── Agent → specialist (design,      Opus 4-7)
                  └── Agent → specialist (security,    Opus 4-7)
```

The review skill runs inline in the orchestrator's session (so the `Agent` tool
is available to spawn the three parallel specialists); specialists run on
`claude-opus-4-7` regardless of the orchestrator's own model.

All agents access ContextMatrix via MCP tools over HTTP (`POST /mcp`).

**Agents MUST always use MCP tools for all ContextMatrix interactions.** This
means `claim_card`, `heartbeat`, `update_card`, `complete_task`, etc. — never
`curl`, `wget`, or any direct REST API call. Direct HTTP is for human developers
verifying API handler code; it is not a supported interface for agent board
operations. This rule is enforced in the `workflowPreamble` injected into every
skill prompt and is explicitly stated in each skill file's Rules section.

## Workflow Skill files

Skill files are markdown documents in `workflow-skills/`. They serve two
purposes:

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
  parallel via the `Agent` tool — and only the top-level (calling) session has
  the `Agent` tool; sub-agents spawned via `Agent` do not get `Agent`
  themselves. If the review ran in a spawned sub-agent it would silently degrade
  to a single-perspective walkthrough because the parallel spawn would not
  happen. The synthesizer runs on the orchestrator's own model (often Sonnet);
  the three specialists each run on `claude-opus-4-7`. Do not reintroduce the
  model-match gate on `start_review` — it would reproduce the regression.
  (`get_skill('review-task')` still uses the model-match logic for any
  out-of-band callers; the workflow always goes through `start_review`.)

```
workflow-skills/
  create-task.md          # /contextmatrix:create-task (slash command + skill)
  init-project.md         # /contextmatrix:init-project (slash command + skill)
  refresh-knowledge.md    # /contextmatrix:refresh-knowledge (slash command + skill, human-only)
  create-plan.md          # skill only (loaded via get_skill / start_workflow)
  execute-task.md         # skill only
  review-task.md          # skill only (loaded via start_review or get_skill)
  document-task.md        # skill only
  run-autonomous.md       # skill only (routed to by start_workflow when autonomous)
  brainstorming.md        # skill only
  systematic-debugging.md # skill only
                          # /contextmatrix:start-workflow (server-side only — no skill file)
```

Four slash commands exist: `create-task`, `init-project`, `start-workflow`, and
`refresh-knowledge`. Phase-specific skills (`create-plan`, `execute-task`,
`review-task`, `document-task`, `run-autonomous`, `brainstorming`,
`systematic-debugging`) are loaded by the orchestrator via `get_skill` (or
`start_review` for the review-entry transition); they are not user-facing entry
points. This mirrors how `brainstorming` and `systematic-debugging` have always
worked. `validSkillNames` in `internal/mcp/prompts.go` lists the complete set
addressable by `get_skill`.

`start-workflow` has no skill file. It exists as both a **prompt** (slash
command) and a **tool** (`start_workflow`). Both are server-side only: they
fetch the card, inspect the `autonomous` flag, and return the full skill content
for `run-autonomous` or `create-plan`. The tool enables natural-language
triggering — when a user writes "start workflow for ALPHA-001" (without a slash
command), the agent sees the `start_workflow` tool and calls it to get the
executable workflow content directly. If the card cannot be found, both paths
return an error.

## Task skills

In addition to the workflow skills served via MCP prompts (lifecycle
scaffolding), the runner mounts a curated set of **task skills** at
`~/.claude/skills/` in the worker container. These are standard Claude Code
skills with `SKILL.md` files, discovered by the model via the native Skill tool
and engaged when their descriptions match the work being done.

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
3. else mount the full curated set from `task_skills.dir` (the `dir` subfield of
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
- `create-plan`: no `## Specialist skills` section (runs inline on the
  orchestrator; engagement is governed by run-autonomous when invoked from
  there).
- `create-task`, `init-project`: no `## Specialist skills` section
  (interview/bootstrap, no implementation work).

### Description-writing convention

Skills are engaged by description match. Authors should anchor descriptions in
**observable activities and file types**, not subject areas. See
[`task-skills/README.md`](../task-skills/README.md) in this repo for the full
convention. Operators who maintain their own task-skills directory (configured
via `task_skills.dir`) typically keep an analogous README alongside their
skills.

## Slash command interface

CC exposes these slash commands via the MCP `prompts` capability:

| Command                            | Argument         | Type               | Description                                                                                                                                                                                            |
| ---------------------------------- | ---------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `/contextmatrix:create-task`       | `description`    | optional free text | Start task creation interview                                                                                                                                                                          |
| `/contextmatrix:init-project`      | `name`           | optional           | Initialize a new project board                                                                                                                                                                         |
| `/contextmatrix:start-workflow`    | `card_id`        | required           | Drive a card through its full lifecycle, routed by the card's `autonomous` flag                                                                                                                        |
| `/contextmatrix:refresh-knowledge` | `project`/`repo` | `project` required | Human-only. Rebuild a project's knowledge-base docs (`architecture.md`, `code-structure.md`, `api-documentation.md`, `glossary.md`). Spawns Sonnet sub-agents and commits via `commit_knowledge_docs`. |

`/contextmatrix:start-workflow` is the canonical entry point: it inspects the
card's `autonomous` flag and routes to `run-autonomous` (autonomous cards) or
`create-plan` (HITL cards). Phase-specific prompts for `create-plan`,
`execute-task`, `review-task`, `document-task`, and `run-autonomous` were
intentionally removed from the slash-command surface — they're internal
orchestration steps, not user entry points. The orchestrator loads each phase's
skill via `get_skill` (or `start_review` for the review-entry transition).

Usage examples:

```
/contextmatrix:create-task I want to create a web page for my demo app
/contextmatrix:create-task there is a bug in the login form validation
/contextmatrix:start-workflow ALPHA-001   # routes to run-autonomous or create-plan automatically
/contextmatrix:init-project my-new-project
/contextmatrix:refresh-knowledge my-project
```

The interview-style prompts (`create-task`, `init-project`, `refresh-knowledge`)
return raw skill content for inline execution by the main agent — no sub-agent
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
agent (CC) runs the interview inline — gathering details from the human,
creating the card on the board, and offering next steps — all without spawning a
sub-agent. This is required because the interview needs multi-turn
back-and-forth with the user, which only works in the main agent's context.

**2. Planning** (loaded internally — orchestrator calls
`get_skill('create-plan')`)

When a user invokes `/contextmatrix:start-workflow` on a HITL card (or the
`start_workflow` MCP tool routes there), the orchestrator runs planning inline
and creates subtasks directly.

The flow is:

0. **Claim the card immediately**: The orchestrator calls `claim_card` as its
   very first action — before any exploration or planning begins. This moves the
   card to `in_progress` at the start of planning, not after subtasks are
   created. The card stays claimed through drafting, user approval, and subtask
   creation.
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

**3. Execution** (loaded internally — orchestrator calls
`get_skill('execute-task')`)

CC spawns sub-agents in parallel (one per ready subtask). Each sub-agent:

1. Calls `get_task_context(id)` — reads everything before touching anything
2. Calls `claim_card(id, agent_id)`
3. Writes `## Plan` to card body, calls `update_card`
4. Works through the task, updating `## Progress` in card body as it goes
5. Calls `heartbeat` after every significant unit of work (mandatory)
6. Calls `complete_task(id, agent_id, summary)` when done
7. Prints structured completion summary (see below)

Main agent awaits all `Agent` tool completions and checks for blockers. **Parent
card state is managed by the service layer and the orchestrator:** when the
first subtask is claimed, the parent transitions `todo → in_progress`. When all
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

**4. Documentation** (loaded internally — orchestrator calls
`get_skill('document-task')`)

Uses a single-phase fire-and-report flow. CC spawns a short-lived documentation
sub-agent that reads the parent card + all subtasks and writes external
documentation (README updates, API docs, architecture notes) directly to disk —
no human approval gate before writing. The sub-agent returns `DOCS_WRITTEN`
immediately with a list of files written. CC presents the summary to the user.
The parent card remains in `in_progress` during this phase.

**5. Review** (loaded internally via the `start_review` MCP tool)

The orchestrator calls `start_review(card_id, agent_id, caller_model)`, which
atomically transitions the parent to `review` AND returns the `review-task`
skill in one call — there is no path to load the review skill without committing
the transition. The response always has `inline: true`; the orchestrator runs
the skill in its own session (see "Server-side inline execution" above for why).
The flow:

- **Pass 1 — Spec compliance and test gate (synthesizer = orchestrator):** the
  orchestrator runs the project test suite and lint, plus a spec / scope check
  against the plan and acceptance criteria. If Pass 1 fails, it skips Pass 2
  entirely, writes findings with `recommendation: revise`, and prints
  `REVIEW_FINDINGS`. No specialists are spawned.
- **Pass 2 — Three parallel specialists:** if Pass 1 succeeds, the orchestrator
  spawns three `Agent` calls in a single message (`model: claude-opus-4-7`,
  `subagent_type: general-purpose`): Correctness (bugs, edges, errors, races,
  test quality), Design & Maintainability (architecture, naming, complexity,
  docs), and Security & Performance (input validation, secrets, CVEs,
  complexity, leaks). Each specialist prompt carries the synthesizer's
  `agent_id` because `report_usage` and `add_log` enforce
  `agent_id == AssignedAgent` — specialists act on the synthesizer's behalf for
  board writes. Before returning, each specialist calls `report_usage` against
  the parent card with its own token consumption (model `claude-opus-4-7`); this
  is what makes the specialists' cost visible on the card. Specialists do not
  claim, transition, or write findings to the card body — they return a
  structured Markdown report with severity-tiered findings.
- **Synthesis (synthesizer = orchestrator):** the orchestrator dedupes
  overlapping findings, applies the strictest-defensible severity, and decides
  the overall recommendation (any Critical → `revise`; Important without
  Critical → typically `revise` unless purely cosmetic; only Minor / none →
  `approve` or `approve_with_notes`). It writes the synthesized
  `## Review Findings` section to the parent card body via `update_card`, calls
  `report_usage` for the synthesizer work, and prints `REVIEW_FINDINGS`. The
  orchestrator does NOT release the claim — it keeps ownership for the next
  phase.
- **User decision (CC handles directly)**: CC reads the card body, presents the
  `## Review Findings` section to the user, and asks for approve/reject. No
  sub-agent — the orchestrator already holds the claim and is alive.
- Based on the user's response, the orchestrator prints one of:
  - `REVIEW_APPROVED` — proceeds to finalization (transitions parent to `done`).
  - `REVIEW_REJECTED` — the rejection loop:
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
`/contextmatrix:start-workflow` slash command (or the `start_workflow` MCP tool)
routes the card to `run-autonomous` automatically and drives the entire
lifecycle using the `run-autonomous.md` skill. The orchestrator model is set by
the invoker — Opus for local autonomous (user's session), Sonnet for the remote
runner (via container config).

## HITL chat surface

HITL runs and the global chat panel both expose a typed message channel back to
a live Claude Code session. Two surfaces exist:

- **Per-card runner messages** —
  `POST /api/projects/{project}/cards/{id}/message` forwards human-typed content
  to a running runner container (`card.runner_status == "running"`). The
  endpoint is human-only (rejects non-human `X-Agent-ID`), bounds content to 8
  KB, and the runner echoes the message back through its session-log stream so
  the UI shows it in the same transcript as the agent's output.
- **Global chat panel** — `/api/chats/*` drives `internal/chat.Manager`, which
  owns a SQLite-backed transcript store (sessions + messages), an idle-TTL
  reaper that warms sessions down to cold, an SSE hub for browser fan-out
  (`GET /api/chats/{id}/stream`), and a runner-log bridge that maps each runner
  log entry to a transcript row. Cold sessions rehydrate from the persisted
  transcript via `transcript.Build` before the container starts; the rehydration
  phase ends on the first user message or an explicit
  `chat_rehydration_complete` call.

**Promote to autonomous from chat.** A human can promote a running HITL card
mid-flight: `POST /api/projects/{project}/cards/{id}/promote` (web UI) or the
`promote_to_autonomous` MCP tool (Claude). Both call
`service.PromoteToAutonomous` first (fail-closed: rejects terminal cards, flips
`autonomous: true`, appends an activity log entry, fires an SSE event); only on
success does the API endpoint fire the runner's `/promote` webhook so the
runner-side stdin message is written. Both surfaces gate on `human:` prefix —
agents cannot self-promote. The runner verifies the flag out-of-band via
`GET /api/v1/cards/{project}/{id}/autonomous` (HMAC-signed) before writing its
canned stdin message.

**Lifecycle phases (create-plan skill, HITL and autonomous):**

```
Phase 0:  Pre-planning Gate      → branch on card shape: maintenance-skip (Branch A), systematic-debugging sub-agent (Branch B, both modes), or brainstorming inline (Branch C, HITL only). Produces a ## Design or ## Diagnosis section before plan drafting.
Phase 1:  Plan Drafting          → inline; drafts plan, updates card body, emits PLAN_DRAFTED
Phase 2:  Plan Approval Gate     → get_card autonomous check; HITL presents plan, autonomous skips
Phase 3:  Subtask Creation       → inline; dedupe then create_card for each subtask
Phase 4:  Execution Gate         → get_card autonomous check; HITL asks to start, autonomous skips
Phase 5:  Execution              → checkout feature branch (branch_name); claim parent; get_ready_tasks; spawn execute-task sub-agents in parallel; aggregate worktree branches onto feature branch when worktree isolation used
Phase 6:  Documentation          → release claim, spawn document-task sub-agent, reclaim after DOCS_WRITTEN
Phase 7:  Review                 → transition to review, run review-task inline (always); orchestrator spawns 3 opus specialists in parallel and synthesizes findings
Phase 8:  Review Decision Gate   → get_card autonomous check; autonomous branches on recommendation, HITL asks
Phase 9:  Commit/Push/PR Gate    → get_card autonomous check; autonomous or remote HITL (CM_INTERACTIVE=1) auto-commits/pushes/PR; local HITL asks
Phase 10: Finalization           → reclaim, report_usage, transition to done, release_card (mandatory)
```

For autonomous cards, `run-autonomous.md` drives the same lifecycle with these
phase labels. run-autonomous starts from the correct phase based on card state:

```
Step 0:  Claim the card        → claim_card called before any exploration begins
Step 1:  Create feature branch → if feature_branch is true and branch_name is set, git checkout -b <branch_name> (or checkout existing); skipped otherwise. Runs before planning or sub-agent spawning.
Step 2:  Load context          → get_knowledge_base + get_task_context once, retained across phases
Phase 1: Plan Drafting         → inline, calls create-plan skill via get_skill (model-matched inline)
Phase 2: Subtask Creation      → inline, orchestrator calls create_card directly
Phase 3: Execution             → spawns execute-task sub-agents in parallel; cherry-picks worktree branches onto feature branch when worktree isolation used
Phase 4: Documentation         → spawns document-task sub-agent (parent in in_progress)
Phase 5: Review                → orchestrator transitions parent to review via start_review, runs review-task inline; spawns 3 opus specialists in parallel and synthesizes findings
Phase 6: Finalization          → transitions parent to done, final report_usage, release_card (mandatory)
```

The orchestrator claims the card and moves it to `in_progress` before
determining the starting phase. If the card is already `in_progress` or
`review`, the claim is still required — the starting-phase table determines
which phase to resume from.

**Guardrails:**

- **Branch protection** — agents must never push to `main` or `master`. The
  `report_push` tool returns a hard error if the branch name is `main` or
  `master`.
- **Maximum review cycles** — when a review returns `revise`, the orchestrator
  calls `increment_review_attempts` and then checks whether the returned count
  is `>= 3`; if so it calls `report_usage`, prints `AUTONOMOUS_HALTED`, and
  stops, requiring human intervention. The skill enforces an at-most-3 cap this
  way. The server applies a higher defense-in-depth cap (`maxReviewAttempts = 5`
  in `internal/service/service.go`) so a manual override can still proceed past
  3 if needed without bypassing the skill gate.
- **Heartbeat-based stall detection** — the orchestrator calls `heartbeat` on
  the parent card every 5 minutes and uses `check_agent_health` to detect and
  respawn stalled sub-agents.
- **Human vetting gate** — cards imported from external sources (GitHub Issues,
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
  claude-haiku-4-5: { prompt: 0.000001, completion: 0.000005 } # $1.00 / $5.00 per MTok
  claude-sonnet-4-6: { prompt: 0.000003, completion: 0.000015 } # $3.00 / $15.00 per MTok
  claude-opus-4-6: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
  claude-opus-4-7: { prompt: 0.000005, completion: 0.000025 } # $5.00 / $25.00 per MTok
```

The `report_usage` call must pass `model` matching one of these keys. The model
used depends on the orchestrator and phase — see the **Model Allocation**
section below for the full breakdown. The `recalculate_costs` tool reprices
cards that have non-zero tokens but zero stored cost (e.g. when usage was
reported without a model name); it only touches qualifying cards and never
overwrites a non-zero cost.

## Model Allocation

The system uses two models: **Opus** (strongest reasoning) and **Sonnet**
(cost-effective workhorse). Haiku is not used in any workflow. The orchestrator
decides whether each phase runs inline or as a sub-agent — the `inline` field
returned by `get_skill` (and by `start_review`, which loads the review-task
skill atomically with the state transition) uses exact model match, but the
orchestrator overrides it for phases where the decision is driven by context
management rather than model compatibility.

### HITL + Local Autonomous (Opus orchestrator)

| Phase            | Model  | Method                                               | Why                                                                                                             |
| ---------------- | ------ | ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Orchestrator     | Opus   | User's session (HITL) or run-autonomous (local auto) | Strongest reasoning for planning, review, and coordination                                                      |
| Planning         | Opus   | Inline on orchestrator                               | Orchestrator already is Opus — no spawn needed, retains plan context for subtask creation                       |
| Subtask creation | Opus   | Inline — calls `create_card()` directly              | Trivial work; spawning a sub-agent costs more in overhead than it saves                                         |
| Execution        | Sonnet | Sub-agent per subtask                                | Context isolation (fresh ~50K vs accumulated 150K+) and parallel execution; Sonnet is 1.67x cheaper at scale    |
| Review           | Opus   | Inline (start_review inline=true, Opus==Opus)        | Devil's advocate reasoning benefits from Opus; inline keeps findings in orchestrator context for human approval |
| Documentation    | Sonnet | Sub-agent                                            | Context isolation — orchestrator has 150K+ accumulated context by this phase; fresh sub-agent starts at ~25K    |

### Remote Runner (Sonnet orchestrator)

| Phase            | Model  | Method                                                            | Why                                                                                |
| ---------------- | ------ | ----------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Orchestrator     | Sonnet | Runner container sets model via `--model` / env var               | Cost control — Opus premium not justified for well-defined protocol                |
| Planning         | Sonnet | Inline on orchestrator                                            | Sonnet 4.6 plans well; inline avoids spawn overhead and retains plan context       |
| Subtask creation | Sonnet | Inline — calls `create_card()` directly                           | Same as HITL — trivial work, no sub-agent needed                                   |
| Execution        | Sonnet | Sub-agent per subtask                                             | Context isolation + parallel execution; same rationale as HITL                     |
| Review           | Opus   | Sub-agent (start_review inline=false, Sonnet!=Opus → spawns Opus) | Only phase where Opus premium pays off — catches issues before costly rework loops |
| Documentation    | Sonnet | Sub-agent                                                         | Context isolation — runner has no human to intervene if context grows too large    |

### Inline/sub-agent decision model

The `inline` field returned by `get_skill` (and by `start_review`) uses **exact
model match** — it returns `true` when the caller's model family matches the
skill's model family AND the skill is on the inline-eligible whitelist
(`review-task`, `create-plan`, `brainstorming`):

- **Planning, subtask creation:** Always inline — orchestrator instructions
  override the inline field. The orchestrator retains context for downstream
  phases.
- **Execution, documentation:** Always sub-agent — orchestrator instructions
  specify this for context isolation and parallel execution. The inline field is
  not consulted.
- **Review:** Follow the inline field returned by `start_review` — this is the
  one phase where model compatibility matters. Opus caller gets `inline: true`
  (Opus==Opus) and runs review directly. Sonnet caller gets `inline: false`
  (Sonnet!=Opus) and spawns an Opus sub-agent. Either way, `start_review` has
  already transitioned the parent card to `review` before returning, so the
  state and the action are atomically tied.

### Why `run-autonomous.md` has no model

The orchestrator model is an operational concern, not a skill concern. Local
autonomous uses whatever model the user runs (typically Opus). The remote runner
sets Sonnet via container configuration (`--model` flag or environment
variable). This separation allows the same skill file to work for both workflows
without code duplication or model override logic.

## Required permissions for target projects

Agents working on code repositories need Claude Code permissions configured in
the target project (e.g., `.claude/settings.local.json`). The remote runner sets
the same allowlist on every worker container — see
`contextmatrix-runner/docker/entrypoint.sh` (`ALLOWED_TOOLS_COMMON` and
`ALLOWED_TOOLS_AUTO_EXTRAS`) for the canonical list. Mirror it locally for HITL
sessions that drive the same skills.

**Claude Code tools (common, always allowed):**

- `Read` — read files
- `Edit` — modify existing files
- `Write` — create new files
- `MultiEdit` — apply batched edits in one call
- `NotebookEdit` — modify Jupyter notebook cells
- `Skill` — engage filesystem-mounted task skills
- `Glob` / `Grep` — search file contents and paths
- `TodoWrite` — track in-flight work
- `WebFetch` / `WebSearch` — fetch external references when researching

**Claude Code tools (autonomous mode only):**

- `Task` — spawn sub-agents via the `Agent` tool. The runner adds this only in
  autonomous mode (no human review gate), so HITL containers cannot spawn
  parallel sub-agents that would bypass approval. Local HITL sessions that drive
  `create-plan` should likewise omit `Task` unless the user is comfortable with
  sub-agents committing without review.

**MCP tools (auto-available via MCP config):** All `mcp__contextmatrix__*` tools
are available once the MCP server is configured. No per-tool allowlisting is
needed for MCP tools, but the runner does enumerate them explicitly in its
allowlist.

**Bash tools (project-specific):** allowlisted by exact command prefix (e.g.,
`Bash(make:*)`). The runner enables a baseline that covers Git/GitHub
(`Bash(git:*)`, `Bash(gh:*)`), Go (`Bash(go test:*)`, `Bash(go build:*)`,
`Bash(go vet:*)`, `Bash(go mod:*)`, `Bash(go run:*)`, `Bash(go install:*)`),
Make (`Bash(make:*)`), Node/npm (`Bash(npm:*)`, `Bash(node:*)`, `Bash(npx:*)`),
Python (`Bash(python3:*)`, `Bash(pip3:*)`), filesystem basics (`Bash(mv:*)`,
`Bash(cp:*)`, `Bash(rm:*)`, `Bash(mkdir:*)`, `Bash(ls:*)`, `Bash(find:*)`,
`Bash(which:*)`, `Bash(command:*)`), and text inspection (`Bash(cat:*)`,
`Bash(head:*)`, `Bash(tail:*)`, `Bash(wc:*)`, `Bash(echo:*)`,
`Bash(printenv:*)`, `Bash(sed:*)`, `Bash(awk:*)`, `Bash(grep:*)`,
`Bash(sort:*)`, `Bash(uniq:*)`, `Bash(diff:*)`, `Bash(tr:*)`, `Bash(cut:*)`,
`Bash(tee:*)`, `Bash(xargs:*)`, `Bash(date:*)`, `Bash(jq:*)`). Trim or extend
this list to match the project's language and build system.

If `Edit` or `Write` (or another tool an execution agent needs) is missing from
the target project's allowlist, the agent will report `TASK_BLOCKED` with an
actionable error message explaining what permissions are needed. The user must
update the project's permissions config before retrying.

## Knowledge Base Tools

### `get_knowledge_base`

Returns all knowledge-base docs for a project in a single call. Used by
planning, brainstorming, and debugging skills to load architectural context.

**Input:**

| Field     | Required | Description                             |
| --------- | -------- | --------------------------------------- |
| `project` | yes      | Project name                            |
| `repo`    | no       | Repo name; defaults to the primary repo |

**Response:**

```json
{
  "project": "my-project",
  "repo": "primary",
  "docs": {
    "architecture.md": "...",
    "code-structure.md": "...",
    "api-documentation.md": "...",
    "glossary.md": "..."
  },
  "summaries": {
    "architecture.md": "Short summary extracted from the doc's ## Summary section.",
    "code-structure.md": "Short summary...",
    "api-documentation.md": "",
    "glossary.md": "Short summary..."
  },
  "meta": { "...": "..." }
}
```

**`summaries` field:** a map from doc name to the text of its first `## Summary`
section. Empty string when a doc has no `## Summary` section. Always present
(never `null`) — an empty object `{}` means no docs have summaries yet.

**Usage pattern for agents:** when `summaries` is non-empty, read each doc's
summary to judge relevance to the current task, then retain full content from
`docs` only for the relevant docs. When `summaries` is empty or a doc has no
entry, load all docs as before (current fallback behaviour).

### `read_knowledge_doc`

Read a single KB doc by name. Use when you need only one doc and want to avoid
loading the full payload.

**Input:** `project` (required), `repo` (optional), `doc` (required — one of
`architecture.md`, `code-structure.md`, `api-documentation.md`, `glossary.md`).

### `list_knowledge_bases`

Enumerate KB summaries across all projects (or a single project). Returns
project name, repos, and per-doc human-edited flags so the refresh skill can
warn before overwriting human edits.

**Input:** `project` (optional — omit to enumerate every project).

### Human-only refresh tools

The `refresh-knowledge` skill orchestrates a rebuild of the four KB docs. These
tools enforce `agent_id` starting with `human:` and reject other callers
(`agent_id must start with 'human:' and have a non-empty suffix`). Agents cannot
self-refresh.

- **`refresh_knowledge_base`** — returns a build plan (per-doc work items,
  human-edited flags, cost estimate). Does not run sub-agents; the skill spawns
  those.
- **`commit_knowledge_docs`** — atomically writes the produced docs and commits
  them with a single message keyed by `head_commit` (the target repo HEAD SHA at
  refresh time). Clears the `human_edited` flag on each written doc.
- **`update_refresh_progress`** — runner-mode only. Reports per-doc progress
  (docs_total / docs_done / current_doc) for a refresh job running inside the
  runner container so the operator UI shows live status. Returns
  `tracked: false` when no in-flight job matches; local mode invocations are
  intentional no-ops.
