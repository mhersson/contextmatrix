# Create Plan

## Agent Configuration

- **Model:** claude-opus-4-6 — Planning shapes everything downstream; worth the
  cost.

---

You are helping a human break down a task into an executable plan with subtasks.
The card context is provided above — read it carefully before proceeding.

## Step 0: Claim the card

Call `claim_card(card_id, agent_id)` to mark the card as actively being planned.
This makes the planning visible in the UI (pulsating border + agent badge). If
the card is in `todo`, it auto-transitions to `in_progress`.

## Step 1: Understand the task

Review the card details provided above. If the card body already contains
requirements or notes, use them as input. If the task is underspecified, ask the
human clarifying questions before planning.

Call `get_task_context` to fetch the full card with project config, parent card,
and sibling context if needed.

## Step 2: Draft the plan

Break the work into subtasks following these rules:

- Each subtask should be completable by a single agent in roughly **one focused
  session** (~2 hours of work or less)
- Each subtask should touch at most **2-3 files** — if it touches more, split it
  further
- Subtasks should be **independently verifiable** — each one should produce a
  testable result
- Set `depends_on` correctly — a subtask that needs another subtask's output
  must declare the dependency
- Order subtasks so that independent ones can run **in parallel**
- Write clear, specific titles — an agent reading only the title should
  understand the scope
- Include acceptance criteria or key details in each subtask's body
- Each subtask must include its own tests — do not create separate "write tests"
  subtasks. Tests are part of the work, not an afterthought.
- Do not over-engineer the plan. Solve the problem at hand — no speculative
  abstractions, no unnecessary indirection, no premature generalization.
- Do not include documentation subtasks — external documentation is handled by a
  dedicated documentation agent after the work is reviewed.

Present the plan to the human in this format:

```
## Plan for ALPHA-001: Add JWT auth middleware

1. SUBTASK: Implement JWT token generation and validation
   Type: task | Priority: high | Labels: [backend, security]
   Depends on: (none)
   Body: Create jwt.go with Sign() and Verify() functions. Use RS256. Add unit tests.

2. SUBTASK: Add auth middleware to HTTP router
   Type: task | Priority: high | Labels: [backend]
   Depends on: subtask 1
   Body: Create middleware that extracts Bearer token, calls Verify(), sets user context. Return 401 on failure.

3. SUBTASK: Add login endpoint
   Type: task | Priority: high | Labels: [backend, api]
   Depends on: subtask 1
   Body: POST /api/login accepting username/password, returning JWT. Add integration test.
```

## Step 3: Iterate

Ask the human for feedback on the plan. Adjust subtask scope, ordering, or
details based on their input. Repeat until the human approves.

## Step 4: Create subtasks

Once approved:

1. Call `update_card` to write the approved plan into the parent card body under
   a `## Plan` section.
2. Call `create_card` for each subtask with:
   - `parent` set to the parent card ID
   - `depends_on` set to the IDs of prerequisite subtasks (use the card IDs
     returned from previous `create_card` calls)
   - Clear title, type, priority, labels, and body as discussed

Confirm all subtasks created. List them with their IDs:

```
Created 3 subtasks for ALPHA-001:
  ALPHA-002: Implement JWT token generation and validation
  ALPHA-003: Add auth middleware to HTTP router (depends on ALPHA-002)
  ALPHA-004: Add login endpoint (depends on ALPHA-002)
```

## Step 4b: Report token usage

Call `report_usage` with:
- `card_id`: the parent card ID you are planning
- `agent_id`: your agent ID
- `model`: `"claude-opus-4-6"` (must match the model in Agent Configuration above)
- `prompt_tokens` / `completion_tokens`: your estimated token consumption for this planning session

This tracks the cost of planning sessions, which use Opus and are significant.

## Step 4c: Release the card

Call `release_card(card_id, agent_id)` to release your claim now that planning is
done. The card stays in `in_progress` with subtasks ready for execution.

## Step 5: Offer execution

Ask the human:

> The plan is ready. Would you like to start executing these tasks now? I'll
> spawn agents for all tasks that are ready to go.

If **yes**:

1. Call `get_ready_tasks` for the project to find subtasks with all dependencies
   met (state `todo`, no unfinished deps).
2. For each ready task, call
   `get_skill(skill_name='execute-task', card_id=<id>)`. The response contains
   `model` (which model to use, e.g. `"sonnet"`) and `content` (the full
   prompt). Spawn a sub-agent using the **Agent tool** with:
   - `prompt`: the `content` from `get_skill`
   - `model`: the `model` from `get_skill`
   - `description`: `"execute <card_id>"`
   Spawn all ready tasks **in parallel** (multiple Agent tool calls in one
   message).
3. Monitor sub-agent completions. When a sub-agent finishes and unblocks new
   tasks, call `get_ready_tasks` again and spawn agents for the newly ready
   tasks.
4. When all subtasks are done, call
   `get_skill(skill_name='review-task', card_id=<parent_id>)` and spawn a
   review sub-agent using the Agent tool with the returned `model` and
   `content`.
5. After review approval, call
   `get_skill(skill_name='document-task', card_id=<parent_id>)` and spawn a
   documentation sub-agent using the Agent tool with the returned `model` and
   `content`.

If **no**: let the human know they can run
`/contextmatrix:execute-task <card_id>` for individual tasks or come back later.

## MANDATORY: Complete the full workflow

If the user chooses to execute, you MUST follow through the **entire pipeline**
to completion. Do NOT stop partway:

1. **Execute** — Spawn agents (via `get_skill` + Agent tool) for all ready
   subtasks. Monitor completions. When a subtask finishes and unblocks new
   tasks, spawn agents for the newly ready tasks.
2. **Review** — When ALL subtasks are done, call
   `get_skill(skill_name='review-task', card_id=<parent_id>)` and spawn a
   review sub-agent via the Agent tool with the returned `model` and `content`.
3. **Documentation** — After review approval, call
   `get_skill(skill_name='document-task', card_id=<parent_id>)` and spawn a
   documentation sub-agent via the Agent tool.
4. **Done** — After documentation, transition the parent card to `done`.

Each phase MUST lead to the next. Do NOT create subtasks and then stop. Do NOT
spawn execution agents and then stop. Do NOT complete review and then stop.

The parent card's full lifecycle is:
`todo → in_progress (subtasks start) → review (all subtasks done) → done (after review + docs)`

**Abandoning the workflow mid-stream is never acceptable.** If you cannot
continue (e.g., the user asks to pause), clearly communicate where in the
pipeline you stopped and what must happen next to resume.
