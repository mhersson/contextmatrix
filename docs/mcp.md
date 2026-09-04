# MCP integration

ContextMatrix serves a Model Context Protocol (MCP) server that agents use for
every board interaction. This page is the reference for connecting a client
and for what the server exposes: tools, slash commands, phase skills, and the
payload rules. Orchestration detail lives in
[agent workflow](agent-workflow.md); response shapes live in the
[API reference](api-reference.md).

## Endpoint and authentication

| Item      | Value                                                        |
| --------- | ------------------------------------------------------------ |
| Endpoint  | `POST /mcp` (Streamable HTTP transport)                      |
| Auth      | `Authorization: Bearer <mcp_api_key>` when `mcp_api_key` is set |
| Identity  | `agent_id` argument on each tool; humans use a `human:` prefix |

Claude Code config (`~/.claude.json` for user scope, `.mcp.json` in the
project root for project scope):

```json
{
  "mcpServers": {
    "contextmatrix": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer your-mcp-api-key" }
    }
  }
}
```

Omit the `headers` block when `mcp_api_key` is empty. A wrong or missing
bearer returns 401 with a `WWW-Authenticate: Bearer` challenge. Worker
containers receive the key in their trigger payload and send it on every call;
see [remote execution](remote-execution.md) § Security Model.

## Rules

- Agents interact with the board **only through MCP tools**. Never `curl`, the
  REST API, or direct file edits. If no tool covers an operation, the agent
  reports it blocked.
- Identity is the `agent_id` argument. IDs starting with `human:` are human
  callers; everything else is an agent.
- Unvetted external cards (imported issues with `vetted: false`) return a
  redacted body to non-human callers and are skipped by `get_ready_tasks`.
- One agent per card: mutations on a claimed card require the claim holder's
  `agent_id`.

## Workflow entry point

`start_workflow` (or the `/contextmatrix:start-workflow` slash command) reads
the card's `autonomous` flag and returns the matching workflow skill:
`run-autonomous` for autonomous cards, `create-plan` for human-in-the-loop
(HITL) cards. The content is wrapped in an inline-execution envelope, so the
calling session drives the workflow itself. Run it in a fresh session seeded
with only the card ID; survey output from `list_cards` or `get_ready_tasks`
carried into an execution session is re-billed on every later call.

The orchestrator then chains the phases:

1. **Plan** with `create-plan`; a `plan-draft` sub-agent writes the plan and
   subtasks to the card.
2. **Execute** subtasks in parallel `execute-task` sub-agents: `claim_card`,
   work, `complete_task`; the parent waits with `await_subtasks`.
3. **Document** with `document-task`; the parent stays `in_progress`.
4. **Review** via `start_review`, which moves the parent to `review` and
   returns `review-task` in one call; findings go to the card body.
5. **Finalize**: push the branch, `report_push`, run PR gates, transition to
   `done`, `release_card`, `report_usage`.

Every phase and gate is specified in [agent workflow](agent-workflow.md).

## Tools

The server registers 39 tools. The "Caller" column uses these values:

| Value      | Meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| any        | No caller gate.                                                  |
| human      | `agent_id` must start with `human:`.                             |
| claim      | Caller must hold the card's active claim.                        |
| owner      | Checked against the claim only when the card is claimed.         |
| session    | Chat containers only; the session ID must match the caller's.    |

### Cards

| Tool                  | Description                                                      | Caller |
| --------------------- | ---------------------------------------------------------------- | ------ |
| `list_cards`          | List card summaries filtered by state, type, label, agent, parent | any    |
| `get_card`            | One full card; optional sections filter, activity log, images    | any    |
| `get_task_context`    | Card + parent (full), siblings (summaries), project config       | any    |
| `get_ready_tasks`     | Unclaimed `todo` cards whose `depends_on` are all `done`         | any    |
| `get_subtask_summary` | Subtask counts by state for a parent                             | any    |
| `check_agent_health`  | Heartbeat age and status per subtask agent                       | any    |
| `await_subtasks`      | Block until subtasks finish or one stalls; refreshes the heartbeat | any  |
| `create_card`         | Create a card; subtask when `parent` is set; lints self-containment | any |
| `update_card`         | Patch mutable fields, upsert one H2 section, set `autonomous`    | owner  |
| `transition_card`     | Change state, validated against the project's transitions       | owner  |

### Claim lifecycle

| Tool                        | Description                                                 | Caller |
| --------------------------- | ----------------------------------------------------------- | ------ |
| `claim_card`                | Take the claim, then move `todo` to `in_progress`; a refused move (transitions, unmet `depends_on`) leaves the claim and sets `auto_transition_failed` | any |
| `heartbeat`                 | Refresh liveness; any mutation refreshes it too             | claim  |
| `add_log`                   | Append an activity entry (capped at 50 per card)            | claim  |
| `release_card`              | Drop the claim                                              | claim  |
| `complete_task`             | Log, transition (subtask to `done`, parent to `review`), release | claim |
| `report_push`               | Record branch and PR URL; hard error on `main` / `master`   | claim  |
| `report_parked`             | Set `worker_status: parked` with the reason in the log      | claim  |
| `increment_review_attempts` | Bump `review_attempts`; the server caps it at 7             | claim  |
| `promote_to_autonomous`     | Flip `autonomous` to true; idempotent; 409 on terminal cards | human |

### Workflow skills

| Tool             | Description                                                       | Caller |
| ---------------- | ----------------------------------------------------------------- | ------ |
| `start_workflow` | Workflow skill routed by `autonomous`; always inline              | any    |
| `start_review`   | Transition the parent to `review` and return `review-task` inline | claim  |
| `get_skill`      | Any phase skill with card context; `inline` when the model matches | any   |

### Usage and models

| Tool                     | Description                                                     | Caller |
| ------------------------ | --------------------------------------------------------------- | ------ |
| `report_usage`           | Add tokens, cache tokens, cost, phase and duration to a card    | owner  |
| `recalculate_costs`      | Re-price estimated buckets from the current rate table          | any    |
| `report_incapable_model` | Blacklist a model that could not drive the tool loop            | any    |
| `report_model_outcome`   | Record win / loss / failed rows for Best-of-N or a solo run     | claim  |

### Projects

| Tool             | Description                                                    | Caller |
| ---------------- | -------------------------------------------------------------- | ------ |
| `list_projects`  | All projects with their configs                                | any    |
| `create_project` | Create a board; `boards_repo` picks the repo when several exist | any   |
| `update_project` | Change states, types, priorities, transitions, repo            | any    |
| `delete_project` | Delete a project with zero cards                               | any    |

### Playbooks

Registered unless a project named `playbooks` disables the subsystem.
Mutations require `agent_id` for attribution but have no permission gate.

| Tool                    | Description                                          | Caller |
| ----------------------- | ---------------------------------------------------- | ------ |
| `list_playbooks`        | Slim list view with per-entry status                 | any    |
| `get_playbook`          | Full detail with entries resolved against cards      | any    |
| `create_playbook`       | Create with entries; all-or-nothing; `boards_repo`   | any    |
| `update_playbook`       | Title and description; the id never changes          | any    |
| `delete_playbook`       | Delete; referenced cards are untouched               | any    |
| `add_playbook_entry`    | Append a card reference or manual gate step          | any    |
| `update_playbook_entry` | Done state, note, text, or position                  | any    |
| `remove_playbook_entry` | Remove one entry; its id is never reused             | any    |

### Chat

| Tool                        | Description                                          | Caller  |
| --------------------------- | ---------------------------------------------------- | ------- |
| `chat_rehydration_complete` | End a resumed chat's rehydration phase with a summary | session |

## Slash commands

Workflow skills served as MCP prompts. Claude Code exposes them as
`/contextmatrix:<name>`.

| Command          | Argument      | Required | Description                                   |
| ---------------- | ------------- | -------- | --------------------------------------------- |
| `create-task`    | `description` | no       | Guided card creation with a human interview   |
| `init-project`   | `name`        | no       | Create a project board for the current repo   |
| `start-workflow` | `card_id`     | yes      | Drive a card through its lifecycle, routed by `autonomous` |

## Phase skills

The remaining skill files in `workflow-skills/` are not slash commands. The
orchestrator loads them with `get_skill` (or `start_review` for the
review-entry transition): `brainstorming`, `create-plan`, `document-task`,
`execute-task`, `plan-draft`, `review-task`, `run-autonomous`,
`systematic-debugging`. `get_skill` returns `inline: true` only for
`create-plan`, `brainstorming` and `review-task` when `caller_model` matches
the skill's model; otherwise the orchestrator spawns a sub-agent with the
returned `model`. `start_workflow` and `start_review` are always inline.
Point `workflow_skills_dir` at a copy to customize any skill.

## Payload rules

- **Full vs summary.** `get_card` and `get_task_context` (primary card and
  parent) return the full card. Every other card-returning tool returns a
  summary without `body`, `activity_log` or `usage_breakdown`. `get_card`
  takes `include_activity_log=false` and `sections=[...]` to trim further.
- **Images.** Server-hosted images referenced in the body are attached as
  inline base64 image blocks: at most 10 per call and about 20 MiB in total,
  in body order, later references dropped when over budget. Pass
  `include_images=false` to skip. `get_task_context` attaches images for the
  primary card only. Image scanning runs on the filtered body when `sections`
  is set.
- **Warnings.** `create_card` and `update_card` return `warnings` when the
  body references the author's environment; fix them before proceeding.
- **Remote errors.** On a shared board, push-verified tools (`create_card`,
  `claim_card`, project creation) prefix a failed push with
  `remote unreachable:`; the workflow skills retry on it.

Shapes are in the [API reference](api-reference.md): § Card object and
§ Card list response envelope.

## See also

- [Agent workflow](agent-workflow.md) - phases, gates, sub-agent contracts
- [Running cards](running-cards.md) - the human side of a run
- [Data model](data-model.md) - card fields and human-only rules
- [Remote execution](remote-execution.md) - how containers reach this endpoint
- [Shared boards](shared-boards.md) - claims across instances
