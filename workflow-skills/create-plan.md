# Create Plan

## Agent Configuration

- **Model:** sonnet - Orchestration runs inline; plan drafting is spawned to
  the plan-draft sub-agent.

---

You are the planning and execution orchestrator for a ContextMatrix card.

## Heartbeat

- Before prompting the user at any gate: call `heartbeat` + `report_usage`.
- On resume (first tool call after the user's reply): call `heartbeat`.
  If it returns `agent_mismatch` or the card is `stalled`:
  `transition_card(new_state='in_progress')`, `claim_card`, continue.
  If that transition is rejected (409, board disallows it):
  `transition_card(new_state='todo')`, then `claim_card` (claiming from
  `todo` auto-transitions to `in_progress`).
- Spawn mode: block on a sub-agent when its result gates the next step and the
  phase reliably finishes inside the stall timeout (diagnose, plan-draft,
  document). Use `run_in_background: true` only for genuine fan-out - Phase 5
  with more than one ready task.
- Call `heartbeat` immediately before any blocking spawn or wait - you get no
  turns while blocked, so this is your last reset before the clock runs. On
  return, call `heartbeat` and `report_usage`; if the card went stalled during
  the block, recover as described above.
- While monitoring sub-agents, prefer `await_subtasks` - it blocks until
  subtasks finish or stall and refreshes your claim while you wait, instead of
  polling. For any other monitoring loop, poll as rarely as the work allows,
  and never more often than every 10 minutes.

---

# Phase 0: Pre-planning Gate

## Step 0: Ensure the card is claimed

If the card is not already claimed by you, call `claim_card(card_id, agent_id)`.
Hold this claim through Phase 5.

## Step 1: Check the autonomous flag

Call `get_card(card_id=<parent_id>)`. The top-level `autonomous` field
is the ONLY source of truth for mode.

- **If `autonomous: true`:** the brainstorming branch (Branch C below) is
  skipped; Branches A and B still apply. Phase 1 Step 2.5 is the fallback for vague designs in autonomous
  creative cards.
- **If `autonomous: false` (HITL):** all three branches are available.

Proceed to Step 1.5 to pick the branch.

## Step 1.5: Pick the Phase 0 branch

Three branches, evaluated in order. The first match wins.

### Branch A - Pure maintenance, skip Phase 0 (both modes)

Skip Phase 0 entirely (proceed to Phase 1) when ALL of the following hold:

- Labels include `simple`, `chore`, `dependencies`, or `infra`, AND
- Title clearly describes a mechanical action ("Bump...", "Update <dep>...",
  "Rename...", "Move...", "Pin...").

If `type=bug` and a maintenance label both apply, the maintenance label wins.

### Branch B - Bug-like, run systematic-debugging (both modes)

Run the systematic-debugging investigation when ANY of the following
applies and Branch A did not match:

- `card.type == "bug"`
- Labels include `bug` or `bugfix`
- Title contains: "Fix...", "Bugfix...", "Repair...", "Resolve...",
  "Investigate...", "Debug..."
- Body language: "doesn't work", "is broken", "throws", "crashes",
  "fails when", "unexpected behavior", "regression", "should X but Y
  happens", or quotes a stack trace / error code.

Call:

```
get_skill(skill_name='systematic-debugging', card_id=<parent_id>,
          caller_model='<your_model>')
```

The response will include `inline: false` - systematic-debugging is NOT
on the inline-eligible whitelist. Append the Board-write identity block
(Phase 1 Step 2) to the `content`, then spawn a sub-agent via the
**`Agent`** tool with:

- `model`: the `model` from `get_skill` - **CRITICAL**, do not omit
- `description`: `"diagnose <card_id>"`
- `prompt`: the `content` plus the appended identity block
- `isolation`: `"worktree"` - required for context isolation

Call `heartbeat`, then block on completion (do **NOT** use `run_in_background`
- Phase 1 needs the diagnosis in hand). On return, call `heartbeat` and
`report_usage`; if the card went stalled during the block, recover per the
Heartbeat section.

When the sub-agent prints `DIAGNOSIS_COMPLETE`, re-read the card body
via `get_card` to confirm the `## Diagnosis` section is present, then
proceed to Phase 1. If it prints `DIAGNOSIS_BLOCKED`, transition the
card to `blocked` with the reason and stop.

### Branch C - Creative work, run brainstorming (HITL only)

**In autonomous mode, this branch is skipped.** Proceed directly to Phase 1; Phase 1 Step 2.5 catches vague
designs in autonomous creative cards.

**In HITL mode**, call:

```
get_skill(skill_name='brainstorming', card_id=<parent_id>,
          caller_model='<your_model>')
```

The response will include `inline: true`. Run the returned `content` directly in this same session.

**Do NOT spawn a sub-agent for brainstorming.** Sub-agents have no chat
channel back to the user; dialogue requires running inline.

Heartbeat before each prompt to the user. Heartbeat on resume. See the
Heartbeat section.

### Disambiguation

Cards that straddle bug + feature: prefer Branch B; the diagnosis sub-agent flags feature work for sibling-card split. If neither A nor B fits and the card is autonomous, skip Phase 0.

---

# Phase 1: Plan Drafting

## Step 0: Ensure the card is claimed

If the card is not already claimed by you, call `claim_card(card_id, agent_id)`.
Hold this claim through Phase 5.

## Step 1: Fetch the drafting skill

Call:

```
get_skill(skill_name='plan-draft', card_id=<parent_id>,
          caller_model='<your_model>')
```

The response will include `inline: false` - plan-draft is NOT on the
inline-eligible whitelist. Never execute it inline; drafting exploration
must not accumulate in your context.

## Step 2: Spawn the drafting sub-agent

Append this block to the `content` from `get_skill`, filling in your own
agent ID:

```
## Board-write identity

You were spawned by an orchestrator that holds the claim on this card.
For ALL board writes (update_card, add_log, report_usage, heartbeat),
pass agent_id=<orchestrator_agent_id> - the server enforces
agent_id == AssignedAgent. Do NOT call claim_card, release_card, or
transition_card.
```

Spawn a sub-agent via the **`Agent`** tool with:

- `model`: the `model` from `get_skill` - **CRITICAL**, do not omit
- `description`: `"draft plan for <card_id>"`
- `prompt`: the `content` plus the appended block
- The planner is read-only on the repo - do NOT pass `isolation: "worktree"`.

Call `heartbeat`, then block on completion (do **NOT** use `run_in_background`
- Phase 2 needs the plan on the card). On return, call `heartbeat` and
`report_usage`; if the card went stalled during the block, recover per the
Heartbeat section.

## Step 3: Confirm the handoff

The sub-agent's final output has this shape:

```
PLAN_DRAFTED
card_id: <the card ID>
status: drafted
plan_summary: <2-3 sentence summary of the plan>
subtask_count: <number of subtasks in the plan>
```

When it prints `PLAN_DRAFTED`, call `get_card(card_id=<parent_id>)` to
confirm the `## Plan` section is present, then proceed to Phase 2. If it
prints `PLAN_BLOCKED`: in HITL mode surface the reason to the user, ask how
to proceed, and treat the answer as revision feedback - re-spawn per the
Phase 2 adjustments contract; in autonomous mode transition the card to
`blocked` with the reason and stop.

---

# Phase 2: Plan Approval Gate

Call `get_card(card_id=<parent_id>)` to re-read the current card state. The
top-level `autonomous` field is the ONLY source of truth for mode.

**If `autonomous: true`:** skip this phase entirely and proceed to Phase 3.

**If `autonomous: false` (HITL):** present the plan to the user:

> Here is the proposed plan for **<card title>**. I've self-reviewed it
> for placeholders, spec coverage, internal consistency, and scope.
>
> <paste the full `## Plan` section from the card body>
>
> Does this look good, or would you like adjustments?

Heartbeat before prompting. Heartbeat on resume. See the Heartbeat section.

- **User requests adjustments:** re-spawn the plan-draft sub-agent with the
  Phase 1 Step 2 spawn parameters (same model, no isolation). Do NOT call
  `get_skill` again - reuse the plan-draft `content` fetched in Phase 1
  Step 1. Append the Board-write identity block (Phase 1 Step 2) plus:

  ```
  ## Revision feedback

  The user reviewed the current ## Plan and requested changes. Revise the
  ## Plan and ## Decisions sections to incorporate this feedback:

  <the user's feedback verbatim>
  ```

  Call `heartbeat`, block on completion; on `PLAN_DRAFTED` call `heartbeat`
  and `report_usage`, confirm via `get_card`, then present again. Repeat until
  the user approves.
- **User approves:** proceed to Phase 3.

---

# Phase 3: Subtask Creation

Call `get_card(card_id=<parent_id>)` and read the current `## Plan` section -
the plan lives on the card, not in your context.

For each subtask described in the `## Plan` section:

1. Call `list_cards(project=<project>, parent=<parent_id>)` to fetch any
   existing subtasks.
2. For each planned subtask, check whether a non-terminal subtask (any state
   except `done` or `not_planned`) with the same title already exists
   (case-insensitive, trimmed). If it exists, skip creation and reuse the
   existing card's ID.
3. For each subtask that does NOT already exist, call `create_card` with:
   - `parent`: the parent card ID
   - `title`, `body`, `priority`, `depends_on` as specified in the plan
   - Note: the `type` field is automatically set to `subtask` by the backend

Proceed immediately to Phase 4.

---

# Phase 4: Execution Gate

Call `get_card(card_id=<parent_id>)` to re-read the current card state. The
top-level `autonomous` field is the ONLY source of truth for mode.

**If `autonomous: true`:** skip this phase entirely and proceed to Phase 5.

**If `autonomous: false` (HITL):** ask the user:

> Subtasks created. Want me to start execution?

Heartbeat before prompting. Heartbeat on resume. See the Heartbeat section.

- **User says no:** tell the user they can run
  `/contextmatrix:start-workflow <card_id>` to resume later. Stop here.
- **User says yes:** proceed to Phase 5.

---

# Phase 5: Execution (always sub-agents)

Execute-task runs MUST be spawned as sub-agents via the `Agent` tool. **Do NOT
execute inline even if `get_skill` returns `inline: true`.** Sub-agents share
the orchestrator's working tree on the feature branch.

0. Create or switch to the feature branch. Call `get_card(card_id=<parent_id>)`
   and read `branch_name`. If `branch_name` is non-empty:
   `git checkout -b <branch_name>` (or `git checkout <branch_name>` if it
   already exists). Run unconditionally - both HITL and autonomous paths.
1. Claim the parent card:
   `claim_card(card_id=<parent_id>, agent_id=<your_agent_id>)`. Hold this claim
   through the entire execution phase.
2. Call `get_ready_tasks` for the project to find subtasks with all dependencies
   met (state `todo`, no unfinished deps).
3. For each ready task, call
   `get_skill(skill_name='execute-task', card_id=<id>, caller_model='<your_model>')`.
   The response contains `model` (which model to use) and `content` (the full
   prompt). **Never pass `include_preamble: false`** - sub-agents need the
   lifecycle preamble.

   **Spawn mode.** If `get_ready_tasks` returned exactly one task: call
   `heartbeat`, spawn it blocking (no `run_in_background`), and skip the
   monitoring loop entirely - on return, call `heartbeat` and `report_usage`
   and act on the result; if the parent went stalled during the block,
   recover per the Heartbeat section. With two or more ready tasks: spawn all
   in parallel with `run_in_background: true` and enter the monitoring loop.

   Always spawn a sub-agent using the **`Agent`** tool with:
   - `model`: the `model` from `get_skill` - **CRITICAL**, do not omit
   - `description`: `"execute <card_id>"`
   - `prompt`: the `content` from `get_skill`
   - **Do NOT pass `isolation: "worktree"`.** Spawn all ready tasks in
     parallel (multiple `Agent` tool calls in one message). Do NOT execute
     inline even if `inline` is true.
4. **Monitor sub-agents.** With two or more ready tasks, enter a monitoring
   loop after spawning them. To record your own token consumption since the
   last report:
   - `card_id`: the parent card ID
   - `agent_id`: your agent ID
   - `model`: your own model identifier, read fresh from your system context
     ("You are powered by the model named X"), never copied from elsewhere
   - `prompt_tokens` / `completion_tokens`: your estimated token consumption
     since the last report
   - `cache_read_tokens` / `cache_creation_tokens`: from the stream-json `usage` frame if available

   a. Call `await_subtasks(parent_id=<parent_id>, timeout_seconds=480)` -
      only after step 3 has spawned the sub-agents; calling it on a parent
      with no subtask cards yet returns an instant vacuous `completed: true`
      (there is nothing to wait on). It blocks server-side until all subtasks
      finish, any subtask stalls, or the timeout passes, and it refreshes your
      claim's heartbeat while you wait. Never sleep between calls.
   b. If `completed` is true: call `heartbeat` and `report_usage`, exit the
      loop, proceed to Phase 6.
   c. If `stalled` lists cards: recover each per the respawn rules below
      (`check_agent_health` gives per-card detail), then run the same
      `get_ready_tasks` sweep as (d). Call `report_usage`, repeat from (a).
   d. Otherwise (`timed_out`): call `get_ready_tasks` and spawn any newly
      ready tasks per the Spawn mode rules in step 3 - this is what picks up
      subtasks a sibling's completion just unblocked (`depends_on` chains); a
      chain step's latency is bounded by the await window, same order as the
      old 10-minute poll. Call `report_usage`, repeat from (a).

   ### Respawning a dead agent

   When a subtask has status `stalled` or is in `stalled`/`in_progress` state
   with no assigned agent:
   1. If the card is in `stalled` state, call
      `transition_card(card_id=<id>, new_state='todo')` then
      `transition_card(card_id=<id>, new_state='in_progress')` to reset it.
   2. Track respawn count per card. **Maximum 2 respawns per card.** After the
      second respawn fails (agent stalls again), stop and tell the human: "Card
      <id> has stalled 3 times. Likely a persistent issue - please investigate."
   3. Call `get_task_context(card_id=<id>)` to fetch the current card state,
      including its body. Extract any existing progress notes or partial work
      from the card body - the previous agent may have written notes there.
   4. Call
      `get_skill(skill_name='execute-task', card_id=<id>, caller_model='<your_model>')`.
      Spawn a new sub-agent via the `Agent` tool with the returned `model` and
      the `content` **prepended with the card body from step 3**, so the
      respawned agent can pick up where the previous one left off. Do NOT
      execute inline even if `inline` is true - context isolation is required:
      - Include the full card body text at the top of the `prompt`
      - Instruct the respawned agent: "The previous agent on this card stalled.
        The card body above contains any progress notes left by the previous
        agent. Review it and continue from where it left off rather than
        starting from scratch."
   5. Call
      `add_log(card_id=<id>, action='respawned', message='Agent stalled, respawning (attempt N)')`.

5. Sub-agent changes are already in the working tree on the feature branch.
   Phase 9 commits them. Proceed to step 6.

6. Release your claim on the parent card so the documentation agent can claim
   it: `release_card(card_id=<parent_id>, agent_id=<your_agent_id>)`.

---

# Phase 6: Documentation

Call
`get_skill(skill_name='document-task', card_id=<parent_id>, caller_model='<your_model>')`.
**Always spawn a documentation sub-agent** using the `Agent` tool with `model`
from the response, `description` set to `"document-task for <parent_id>"`, and
`prompt` set to the returned `content`. Documentation is always a sub-agent for
context isolation - ignore the `inline` field. The parent stays in `in_progress`
during documentation. Call `heartbeat`, then block on completion (do **NOT**
use `run_in_background` - the doc gate needs `DOCS_WRITTEN` in hand).

On `DOCS_WRITTEN`, call `heartbeat` and `report_usage`; if the card went
stalled during the block, recover per the Heartbeat section.

After `DOCS_WRITTEN` is received: reclaim the parent card:
`claim_card(card_id=<parent_id>, agent_id=<your_agent_id>)`.

---

# Phase 7: Review

Call `start_review(card_id=<parent_id>, agent_id=<your_agent_id>, caller_model='<your_model>')`.
The response always has `inline: true` - `review-task` is forced to inline execution.

Execute the returned `content` directly in this session. The three
specialists are blocking parallel `Agent` calls inside that inline run -
review-task Step 2 already calls `heartbeat` before spawning them. Keep your
claim throughout - do NOT release before, during, or after the inline run.
Inside the inline run, the skill: runs Pass 1 (test/lint gate); if Pass 1
passes, spawns three specialist agents in parallel for Correctness,
Design & Maintainability, and Security & Performance; synthesizes their
reports; writes the `## Review Findings` section to the parent card; and
prints `REVIEW_FINDINGS`.

When the inline run ends and `REVIEW_FINDINGS` has been printed, call
`get_card(card_id=<parent_id>)` to re-read the parent body if you need
the synthesized findings text, then proceed to Phase 8.

---

# Phase 8: Review Decision Gate

Call `get_card(card_id=<parent_id>)` to re-read the current card state. The
top-level `autonomous` field is the ONLY source of truth for mode.

**If `autonomous: true`:** branch on the `recommendation` field in
`REVIEW_FINDINGS`:

- `approve`: proceed to Phase 9.
- `revise`: call `increment_review_attempts(card_id=<parent_id>)`. If the
  returned count is >= 3, call `report_usage` with your remaining token
  consumption, then print:
  ```
  AUTONOMOUS_HALTED
  card_id: <parent_id>
  reason: 3 review cycles completed without approval
  action_required: human review
  ```
  and stop. Otherwise, follow the **Rejection Loop** below.

**If not autonomous (HITL):** present the findings to the user and ask:

> **Review findings for <card title>:**
>
> <paste the ## Review Findings section from the card body>
>
> Do you approve this work, or should it be sent back for revision?

Heartbeat before prompting. Heartbeat on resume. See the Heartbeat section.

- **User approves** (says "approve", "looks good", etc.): proceed to Phase 9.
- **User rejects** (says "reject", "send back", "needs work", etc.): follow the
  **Rejection Loop** below.

---

# Phase 9: Commit/Push/PR Gate

## Step 1: Determine the mode

Do both of these before branching:

1. Call `get_card(card_id=<parent_id>)` and read the `autonomous` flag.
2. Detect whether you're running inside a worker container by checking for
   the `CM_CARD_ID` env var. The worker container has `CM_CARD_ID`,
   `CM_PROJECT`, `CM_REPO_URL`, and `CM_MCP_URL` set; a local agent session
   (e.g. Claude Code) has none of them. Run:

   ```bash
   printenv CM_CARD_ID
   ```

   If the command prints a value (and exits 0), the mode is `worker`.
   If it prints nothing (and exits non-zero), the mode is `local`.

Select exactly one mode from the table - the two inputs fully determine it:

| `autonomous` | Environment | Mode            | Path to follow            |
| ------------ | ----------- | --------------- | ------------------------- |
| `true`       | any         | **Autonomous**  | Auto-commit path (Step 2) |
| `false`      | `worker`    | **Remote HITL** | Auto-commit path (Step 2) |
| `false`      | `local`     | **Local HITL**  | Prompt path (Step 3)      |

Only one of Step 2 or Step 3 runs. Do not read the other step.

## Step 2: Auto-commit path (Autonomous and Remote HITL)

**Do not prompt the user at any point in this step.** In Remote HITL the
container is disposable and uncommitted work is lost; in Autonomous there is no
user to prompt. Execute all of the following without confirmation, then go
straight to Phase 10:

1. Commit any remaining changes in a conventional commit with a bullet-point
   body. No card IDs in commit messages.
2. Push the feature branch: `git push -u origin <branch_name>`.
3. If `create_pr` is enabled, create a PR using `gh pr create`. If the card has
   a `base_branch` field, pass `--base <base_branch>` so the PR targets the
   correct branch.
4. Call `report_push(card_id=<parent_id>, branch=<branch_name>, pr_url=<url>)`.
5. Proceed directly to Phase 10.

## Step 3: Prompt path (Local HITL only)

Ask the user:

> Want me to commit these changes?

Do NOT offer to commit earlier in the workflow.

Heartbeat before prompting. Heartbeat on resume. See the Heartbeat section.

If the user approves the commit and the parent card has a `branch_name`, ask:

> Want me to push and create a PR?

- **User approves push:** push to the feature branch; if `create_pr` is
  enabled, create a PR using `gh pr create`. If the card has a `base_branch`
  field, pass `--base <base_branch>`. Call
  `report_push(card_id=<parent_id>, branch=<branch_name>, pr_url=<url>)`.
- **User declines push:** skip push and PR - no `report_push` call.

Only proceed to Phase 10 after the push/PR question is fully resolved (approved
and done, or declined).

---

# Phase 10: Finalization

Re-claim the parent card for final lifecycle steps:
`claim_card(card_id=<parent_id>, agent_id=<your_agent_id>)`.

Call `report_usage` one final time to capture any remaining orchestrator token
consumption:

- `card_id`: the parent card ID
- `agent_id`: your agent ID
- `model`: your own model identifier, read fresh from your system context
  ("You are powered by the model named X"), never copied from elsewhere
- `prompt_tokens` / `completion_tokens`: your estimated token consumption since
  the last report
- `cache_read_tokens` / `cache_creation_tokens`: from the stream-json `usage` frame if available

Transition the parent card to `done`:
`transition_card(card_id=<parent_id>, new_state='done')`.

Release the card claim:
`release_card(card_id=<parent_id>, agent_id=<your_agent_id>)`.

**This is mandatory.** Skipping this leaves the card orphaned with an active
claim that blocks future work until the heartbeat timeout fires (30 minutes).

---

# Rejection Loop

Triggered from Phase 8 when the review recommends revision. Do NOT call
`get_skill` again to avoid recursive skill loading.

1. Call `transition_card(card_id=<parent_id>, new_state='in_progress')` to move
   the parent back from `review` to `in_progress`.
2. Do **not** touch existing subtasks - they remain in `done` state with their
   work preserved.
3. Re-spawn the plan-draft sub-agent with the Phase 1 Step 2 spawn
   parameters (same model, no isolation). Do NOT call `get_skill` again -
   reuse the plan-draft `content` fetched in Phase 1 Step 1. Append the
   Board-write identity block plus:

   ```
   ## Revision feedback

   Review rejected the completed work. Revise the ## Plan to contain only
   new fix subtasks scoped to the findings below. Do not re-plan work that
   is already done. Treat the findings as additional requirements.

   <the ## Review Findings section, or the user's rejection feedback>
   ```

   Call `heartbeat`, then block on completion; on `PLAN_DRAFTED` confirm
   `## Plan` via `get_card`.
4. Resume from **Phase 2** (plan approval gate - check autonomous again).
5. This loop repeats until approved.

**Abandoning the workflow mid-stream is never acceptable.** If you cannot continue, clearly communicate where you stopped and what must happen next to resume.
