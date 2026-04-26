# Initialize Project

## Agent Configuration

- **Model:** claude-sonnet-4-6 — Structured interview flow, no deep reasoning
  needed.

---

You are helping a human initialize a new ContextMatrix project board for their
repository. Your goal is to detect sensible defaults from the current
environment and create a well-configured project.

## Step 1: Detect environment

1. Run `git remote get-url origin` to detect the git repository URL. If it
   fails, the current directory may not be a git repo — ask the human for the
   repo URL or leave it blank.
2. Derive a default project name from the repo or directory name. For example,
   `github.com/org/my-app.git` becomes `my-app`. Use the current directory
   basename as fallback.
3. Derive a default prefix from the project name: uppercase, strip hyphens and
   underscores, truncate to a reasonable length (e.g., `my-app` becomes `MYAPP`,
   `project-alpha` becomes `ALPHA`).

## Step 2: Check existing projects

Call `list_projects` to see what projects already exist on the board. If a
project with the detected name already exists, inform the human and ask them to
choose a different name.

## Step 3: Confirm configuration with human

Present the detected defaults and ask the human to confirm or adjust:

```
Project name:  my-app
Prefix:        MYAPP
Repository:    git@github.com:org/my-app.git

States:        [todo, in_progress, blocked, review, done, stalled, not_planned]
Types:         [task, bug, feature]
Priorities:    [low, medium, high, critical]

Transitions:
  todo        -> [in_progress, not_planned]
  in_progress -> [blocked, review, todo]
  blocked     -> [in_progress, todo]
  review      -> [done, in_progress]
  done        -> [todo]
  stalled     -> [todo, in_progress]
  not_planned -> [todo]
```

These are sensible defaults for most projects. The human may want to:

- Change the name or prefix
- Add or remove states (e.g., add `qa` or remove `blocked`)
- Add types (e.g., `chore`, `spike`)
- Modify transitions (e.g., allow `not_planned` from additional states)
- Clear the repo URL if they don't want it tracked

The `stalled` and `not_planned` states are mandatory and cannot be removed.

## Step 4: Create the project

Once the human approves, call `create_project` with the confirmed values.

After creation, confirm to the human:

> Project **my-app** created with prefix **MYAPP**. You can now create tasks
> with `/contextmatrix:create-task` or view the board in the web UI.

## Step 5: Offer next steps

Ask the human:

> Would you like to create your first task now?

- If **yes**: call `get_skill(skill_name='create-task')` and follow the returned
  instructions to guide the human through task creation.
- If **no**: let the human know they can run `/contextmatrix:create-task` later.
