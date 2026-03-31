# Create Plan

You are helping a human break down a task into an executable plan with subtasks.
The card context is provided above — read it carefully before proceeding.

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

## Step 5: Offer execution

Ask the human:

> The plan is ready. Would you like to start executing these tasks now? I'll
> spawn agents for all tasks that are ready to go.

If **yes**:

1. Call `get_ready_tasks` for the project to find subtasks with all dependencies
   met (state `todo`, no unfinished deps).
2. For each ready task, spawn a sub-agent using the **Task tool** with the
   `execute-task` skill prompt and the card's full context. Spawn all ready
   tasks in parallel.
3. Monitor sub-agent completions. When a sub-agent finishes and unblocks new
   tasks, spawn agents for the newly ready tasks.
4. When all subtasks are done, transition the parent card to `review` and spawn
   a review agent using `/contextmatrix:review-task <parent_id>`.
5. After review approval, spawn a documentation agent using
   `/contextmatrix:document-task <parent_id>` to write external docs.

If **no**: let the human know they can run
`/contextmatrix:execute-task <card_id>` for individual tasks or come back later.
