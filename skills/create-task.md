# Create Task

## Agent Configuration

- **Model:** claude-sonnet-4-6 — Structured interview flow, no deep reasoning
  needed.

---

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

Ask the human what they want to do with the new card:

> What would you like to do next?
>
> 1. **Plan it** — Break it into subtasks with create-plan
> 2. **Work on it now** — Execute it immediately with execute-task
> 3. **Leave it on the board** — Pick it up later

**How to invoke a follow-up skill:** Call `get_skill(skill_name=..., card_id=...)`
— it returns a `model` field and `content` field. Use the **`Agent`** tool with
`model` set to the returned model value (**CRITICAL** — do not omit),
`description` set to a short summary, and `prompt` set to the returned content.

- If **plan**: call `get_skill(skill_name='create-plan', card_id='<card_id>')`,
  then use the `Agent` tool with `model` from the response, `description` set to
  "create-plan for <card_id>", and `prompt` set to the returned content.
- If **work on it now**: call
  `get_skill(skill_name='execute-task', card_id='<card_id>')`, then use the
  Agent tool with `model` from the response, `description` set to
  "execute-task for <card_id>", and `prompt` set to the returned content.
- If **leave it**: let the human know they can run
  `/contextmatrix:create-plan <card_id>` or
  `/contextmatrix:execute-task <card_id>` later.

## MANDATORY: Card lifecycle rule

**NEVER start working on a card without following the card lifecycle.** If the
human asks you to "fix it", "do it now", "go ahead", "yes", or any variation —
you MUST either:

1. Call `get_skill(skill_name='execute-task', card_id='<card_id>')` and spawn a
   sub-agent with the returned content, which handles claim, heartbeats, and
   completion automatically, OR
2. Manually follow the full lifecycle: `claim_card` → work with periodic
   `heartbeat` calls → `complete_task`

Starting work without claiming the card leaves it orphaned on the board with no
tracking, no heartbeats, and no completion. **This is never acceptable.**
