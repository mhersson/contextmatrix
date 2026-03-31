# Create Task

You are helping a human create a new task on the ContextMatrix board. Your goal
is to gather enough detail to create a well-defined card that an agent can later
pick up and execute without ambiguity.

## Step 1: Gather information

If a user description was provided above, use it as your starting point.
Otherwise, ask the human what they need done.

Interview the human to fill in these fields. Ask follow-up questions if anything
is unclear or underspecified. Do not ask about fields the human has already
answered — extract what you can from their description first, then ask only
about what's missing.

**Required:**

- **Title** — short, imperative sentence (e.g., "Add JWT auth middleware", "Fix
  login form validation")
- **Project** — which project board to create the card on. Call `list_projects`
  to see available projects if unsure.
- **Type** — `task`, `bug`, or `feature` (confirm with available types from the
  project config)
- **Priority** — `low`, `medium`, `high`, or `critical`

**Optional (ask if relevant):**

- **Labels** — categorization tags (e.g., `backend`, `frontend`, `security`)
- **Dependencies** (`depends_on`) — card IDs that must be completed before this
  task can start
- **Context files** — source files or docs relevant to the task (e.g.,
  `src/auth/`, `docs/api-spec.md`)
- **Parent** — parent card ID if this is a subtask
- **Body** — additional description, requirements, acceptance criteria

## Step 2: Confirm and create

Present a summary of the card to the human for confirmation:

```
Project:  project-alpha
Title:    Add JWT auth middleware
Type:     task
Priority: high
Labels:   [backend, security]
Context:  [src/auth/, docs/auth-spec.md]
```

If the human approves, call `create_card` with the gathered fields. Include any
body content as the card body — write clear requirements and acceptance
criteria.

After creation, confirm the card ID to the human (e.g., "Created ALPHA-007").

## Step 3: Offer next steps

Ask the human:

> Would you like to create a plan and subtasks for this card now? I can break it
> down into executable pieces.

- If **yes**: invoke `/contextmatrix:create-plan <card_id>` with the new card
  ID.
- If **no**: let the human know they can run
  `/contextmatrix:create-plan <card_id>` later.
