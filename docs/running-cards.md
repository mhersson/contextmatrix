# Running cards

How a human starts, steers, stops and pays for a card run from the web UI.
The wire protocol and container lifecycle are in
[remote execution](remote-execution.md); model picks are in
[model selection](model-selection.md); field semantics are in the
[data model](data-model.md); phase orchestration is in
[agent workflow](agent-workflow.md).

## Starting a run

A card in `todo` shows one run button in the card panel header. The
**Autonomous mode** checkbox in the card's Automation tab decides which:

| Checkbox state  | Button       | Mode                                   |
| --------------- | ------------ | -------------------------------------- |
| checked         | **Run Auto** | full lifecycle, no gates               |
| unchecked       | **Run HITL** | per-card chat with approval gates      |

Runs are human-only and need an enabled task backend. Every run gets a fresh
container, the card's generated branch, and a claim held by the worker.

## HITL runs

- The worker starts plan drafting immediately and pauses at its gates: plan
  approval, the execution decision, and review.
- The card's **Chat** tab shows the live transcript with a compose row while
  the worker runs. Messages are capped at 8 KiB.
- **Switch to Autonomous** in the chat footer promotes the session: the card's
  `autonomous` flag flips, the backend confirms it fail-closed, and the worker
  skips the remaining gates. The same operation is `promote_to_autonomous`
  over MCP and `POST .../promote` over REST; all three are human-only and
  idempotent, and reject terminal cards.

## Autonomous runs

```
plan -> subtasks -> execute (parallel) -> document -> review -> done
```

The server forces `interactive` off for autonomous cards, so a stray HITL
trigger cannot open gates on them. The transcript still streams to the Chat
tab, captioned "Autonomous run - read-only" while running and "Session ended -
read-only" afterwards. A card claimed through another instance shows
"Running on <instance> - read-only".

## Fast path

A card labeled `simple` with no existing subtasks is classified
`Complexity: simple` when the workflow skill is built. The agent then skips
planning, subtask creation, documentation and review: claim, feature branch,
do the work, run the project's tests (one fix-and-retry), commit, push, PR
when `create_pr` is on, `report_push`, PR gates, `report_usage`, `done`,
release. Claims, heartbeats, tests, branch protection and release are never
skipped. A card that already has subtasks is `standard` regardless of label.

## Guardrails

| Guardrail            | Rule                                                                 |
| -------------------- | -------------------------------------------------------------------- |
| Branch protection    | `report_push` returns a hard error for `main` or `master`. Skills also forbid the push itself. |
| Review cycles        | `run-autonomous` budgets 3 passes: at `review_attempts >= 3` it files follow-up cards, logs `review_exhausted`, moves the card to `stalled` and halts. `create-plan` halts at the same count. |
| Server ceiling       | `increment_review_attempts` refuses once `review_attempts` reaches 7. |
| Stall detection      | No heartbeat for `heartbeat_timeout` (default 30m, scanned every `stalled_check_interval`, default 1m) moves the card to `stalled` and clears the claim. Any card mutation refreshes the heartbeat. |
| Sub-agent health     | `check_agent_health` reports `warning` at half the timeout and `stalled` at the timeout; `await_subtasks` returns as soon as a subtask stalls. |

## Execution fields

All fields except `autonomous` and `skills` are human-only; the MCP
`update_card` tool does not expose them.

| Field                  | Effect                                                          |
| ---------------------- | --------------------------------------------------------------- |
| `autonomous`           | Routes `start_workflow` to `run-autonomous`; forces HITL off.   |
| `best_of_n`            | 2 or more races that many candidates; clamped to `best_of_n.max_candidates`. |
| `max_capability`       | Ignore price inside the tier and bypass favorites ("Maximum capability" checkbox). |
| `mob_participants`     | 2 or more convenes that many discussion seats; clamped to `mob.max_participants`. |
| `mob_phases`           | Subset of `plan`, `review`, `execute`; execute checkpoints drop `best_of_n`. |
| `mob_guests`           | Names from the `mob.guests` registry joining the discussion.    |
| `verify`               | Command, timeout and env names for the verify gate; card over project, field by field. |
| `create_pr`            | Open a pull request after the push (default true for top-level cards, false for subtasks). |
| `await_ci`             | Stay in `review` until PR checks pass, up to 3 fix rounds, else park. |
| `await_copilot_review` | Request a Copilot review, triage, fix, up to 3 rounds; skipped when unavailable. |
| `model_orchestrator`, `model_coder`, `model_reviewer` | Pin a model per role; a pin beats every selector rule. |
| `skills`               | Task skills mounted in the container; absent inherits, `[]` mounts none. |
| `branch_name`, `base_branch` | Generated feature branch (immutable); optional PR target. |

## Best-of-N

One container. After the plan phase the agent cuts N worktrees from the
plan-approved base, gives each a distinct auto-selected coder model and its own
budget, and races them. A judge (reviewer role, complex tier) picks the winner;
only the winner's branch is pushed, the rest are removed. Outcomes land in the
ledger through `report_model_outcome`.

## Mob sessions

One container hosts every internal seat behind a loopback A2A server and dials
registered guests over the same wire. Each phase in `mob_phases` runs a blind
round then critique rounds, and the decision model synthesizes the answer into
the phase's normal output. Discussions degrade to the solo path on quorum loss,
engine errors or an exhausted mob budget. The transcript streams to the Chat
tab.

## PR gates

With `await_ci` or `await_copilot_review` set, the run enters `pr_gates`
after `report_push` and stays in `review` until the gates pass. Exhausted
rounds, a timeout, or a failed PR creation on a gated card park the card
instead of completing it. See [data model](data-model.md) § PR gates.

## Parked cards

`report_parked` sets `worker_status: parked`, appends a `parked` activity
entry with the reason, and publishes `worker.parked`. The container then exits
and its `completed` callback keeps the parked status while clearing the claim.
The card stays in `review` at phase `pr_gates` with a `## PR Gates` body
section, and the board shows a yellow "Parked for a human" badge. A human
re-triggers the card once the gate can pass; the next `queued` replaces
`parked` and the fresh container resumes at the gate.

## Worker status

| `worker_status` | Set by                     | Badge                     |
| --------------- | -------------------------- | ------------------------- |
| `queued`        | server, at trigger         | "Queued for worker"       |
| `running`       | backend status callback    | "Worker running" (pulse)  |
| `completed`     | backend status callback    | none                      |
| `failed`        | backend, or a failed trigger | "Worker failed"         |
| `killed`        | server, on stop / stop-all | "Worker killed"           |
| `parked`        | `report_parked`            | "Parked for a human"      |

A terminal status clears `assigned_agent` and `last_heartbeat` and flushes
deferred commits. A stalled or failed red badge outranks the parked yellow.

## Stopping

| Control            | Where                     | Effect                                             |
| ------------------ | ------------------------- | -------------------------------------------------- |
| **Stop**           | card panel header         | Kill webhook; `worker_status: killed`; uncommitted work is lost. |
| **Stop All**       | board header, with confirm | Kills every queued or running container in the project. |
| Kill switch        | `config.yaml`             | Disable or remove the task backend: the run button disappears and triggers return `503 BACKEND_DISABLED`. Restart required. |

A card claimed through another instance shows "Running on <instance>" instead
of Stop, and Stop All skips it; that instance owns the container.

## Cost tracking

Workers call `report_usage` with prompt, completion and cache tokens, the
model, an optional provider-reported cost, and whether the counts are
self-estimated or collector-measured. Rates come from `token_costs` or the
endpoint catalog. Where it shows:

- Card **Info** tab, "Models used": per agent and model, with a total that
  includes subtasks and a marker when any bucket is estimated.
- Dashboard: "Cost by model" and "Top cards" over 30 days.
- `recalculate_costs` (MCP) or `POST .../recalculate-costs` re-prices
  estimated buckets after a rate change; actual costs are never touched.

## See also

- [Remote execution](remote-execution.md) - webhooks, lifecycle, security
- [Agent workflow](agent-workflow.md) - phases, gates, skills
- [Model selection](model-selection.md) - tiers, pins, favorites
- [Data model](data-model.md) - every card field
- [MCP integration](mcp.md) - the tools the worker calls
- [Shared boards](shared-boards.md) - runs on other instances
