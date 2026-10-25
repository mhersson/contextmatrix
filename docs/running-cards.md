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

The `run_card` MCP tool starts the same run as **Run Auto** and is human-only.
It sets a non-autonomous `todo` card autonomous first, so HITL runs start only
from the board. A card whose worker is already `queued` or `running`, or an
unvetted import, is left unchanged and the run is refused.

The `run_playbook` MCP tool is the playbook counterpart, also human-only: it
makes a playbook runnable if it is not, then runs it (see
[Playbook runs](#playbook-runs)).

An imported card (one with a `source`) that is not vetted cannot run: the run
button answers `403 CARD_NOT_VETTED` until a human ticks **Content vetted**.

## HITL runs

- The worker starts plan drafting immediately and pauses at two gates: plan
  approval and the review decision. A reply that is not an approval is treated
  as feedback and the phase loops. A creative card (neither pure maintenance
  nor bug-like) also opens a brainstorm dialogue before planning.
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

## Labels

The agent backend always runs its code-driven phases; no label skips planning,
subtasks, documentation or review. The `simple`, `chore`, `dependencies` and
`infra` labels, together with a mechanical title verb (`bump`, `update`,
`rename`, `move`, `pin`, `upgrade`, `downgrade`), mark a card as pure
maintenance, which skips the root-cause investigation that bug-like cards get
before planning. The `simple` fast path that skips planning and review belongs
to the local `run-autonomous` workflow skill; see
[agent workflow](agent-workflow.md#autonomous-mode).

## Guardrails

| Guardrail            | Rule                                                                 |
| -------------------- | -------------------------------------------------------------------- |
| Branch protection    | `report_push` returns a hard error for `main` or `master`. Skills also forbid the push itself. |
| Review cycles        | The review loop runs up to 3 rounds by default (the backend's review attempts cap, at most 6), the last of which is an authoritative pass; each verify-red round adds one round, within the server ceiling. If findings remain it parks the card (see [Parked cards](#parked-cards)) instead of looping on. The local `run-autonomous` skill has its own 3-pass budget that stalls the card. |
| Server ceiling       | `increment_review_attempts` refuses once `review_attempts` reaches 7; the backend parks the card when it does. |
| Stall detection      | No heartbeat for `heartbeat_timeout` (default 30m, scanned every `stalled_check_interval`, default 1m) moves the card to `stalled` and clears the claim. Any card mutation refreshes the heartbeat. |
| Sub-agent health     | `check_agent_health` reports `warning` at half the timeout and `stalled` at the timeout; `await_subtasks` returns as soon as a subtask stalls. |

## Execution fields

All fields except `autonomous` and `skills` are human-only; the MCP
`update_card` tool does not expose them. Project Settings → Card defaults
pre-fills autonomous, maximum capability, mob seats/phases, and the CI /
Copilot PR gates (`await_ci`, `await_copilot_review`) for new cards;
`merge_pr` has no default. Best-of-N stays per card.

| Field                                                 | Effect                                                                                                                                       |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `autonomous`                                          | Routes `start_workflow` to `run-autonomous`; forces HITL off.                                                                                |
| `best_of_n`                                           | 2 or more races that many candidates; clamped to `best_of_n.max_candidates`.                                                                 |
| `max_capability`                                      | Ignore price inside the tier and bypass favorites ("Maximum capability" checkbox).                                                           |
| `mob_participants`                                    | 2 or more convenes that many discussion seats; clamped to `mob.max_participants`.                                                            |
| `mob_phases`                                          | Subset of `plan`, `review`, `execute`; execute checkpoints drop `best_of_n`.                                                                 |
| `mob_guests`                                          | Names from the `mob.guests` registry joining the discussion.                                                                                 |
| `verify`                                              | Command, timeout and env names for the verify gate; card over project, field by field.                                                       |
| `create_pr`                                           | Open a pull request after the push (default from the project's card_defaults; built-in true for top-level cards, always false for subtasks). |
| `await_ci`                                            | Stay in `review` until PR checks pass, up to 3 fix rounds, else park.                                                                        |
| `await_copilot_review`                                | Request a Copilot review, triage, fix, up to 3 rounds; skipped when unavailable.                                                             |
| `merge_pr`                                            | With `await_ci`: merge the PR (merge commit) into its base branch once the CI gate passes, else park. Agent backend only; skill-driven runs leave the PR open. |
| `model_orchestrator`, `model_coder`, `model_reviewer` | Pin a model per role; a pin beats every selector rule.                                                                                       |
| `skills`                                              | Task skills mounted in the container; absent inherits, `[]` mounts none.                                                                     |
| `branch_name`, `base_branch`                          | Predicted feature branch, reconciled by the first `report_push`; optional PR target.                                                          |

A card that belongs to a runnable playbook has `autonomous`, `create_pr`,
`await_ci`, `merge_pr` and `base_branch` set by that playbook and locked; the
card panel shows them read-only as "Set by playbook <title>". See
[playbooks](playbooks.md#running-a-playbook).

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

## POC projects

A project with `poc: true` (Project Settings, Execution tab) runs its cards on
a lighter bar meant for proofs of concept. CM puts the flag on the trigger at
every launch, so a run keeps the value it started with and the first run
after a switch-off gets the full workflow.

- **Review.** One generic reviewer per round replaces the correctness, design
  and security specialists, the authoritative pass included. Bugs,
  exploitable vulnerabilities (a committed secret included), broken or vacuous
  tests and out-of-scope changes block. Missing edge-case tests, coverage,
  maintainability, design taste and security hardening never block and never
  reach a fix request, and the post-approval cleanup pass does not run.
- **Tests.** The planner and coder write tests for the main behaviour the
  card names, not every edge case.
- **Mob review wins.** A card with mob review (`mob_participants >= 2` and
  `mob_phases` empty or containing `review`) keeps its full review: mob
  seats, the three-specialist authoritative pass, the cleanup pass and
  unfiltered fix requests. Its test scope still follows the flag.
- **Unchanged.** Both verify gates, models and tiers, budget, turn limits,
  the review attempts cap, PR gates, the document phase and Best-of-N.
- **Local runs.** `review-task`, which `run-autonomous` and `create-plan` run
  inline, reads `poc` from `get_task_context` and runs one generic reviewer on
  the same bar unless the card has mob review.

## PR gates

With `await_ci` or `await_copilot_review` set, the run enters `pr_gates`
after `report_push` and stays in `review` until the gates pass. Exhausted
rounds, a timeout, or a failed PR creation on a gated card park the card
instead of completing it. With `merge_pr` (agent backend only; skill-driven
runs leave the PR open), a passed CI gate is followed by the merge before the
card completes; a merge that GitHub refuses parks the card. See [data model](data-model.md) § PR gates.

## Parked cards

`report_parked` sets `worker_status: parked`, appends a `parked` activity
entry with the reason, and publishes `worker.parked`. The container then exits
and its `completed` callback keeps the parked status while clearing the claim.
The card stays in `review` at phase `pr_gates` with a `## PR Gates` body
section, and the board shows a yellow "Parked for a human" badge. A human
re-triggers the card once the gate can pass; the next `queued` replaces
`parked` and the fresh container resumes at the gate.

A review loop that ends with findings remaining parks the card the same way
through `report_parked`, leaving it in `review`.

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

| Control             | Where                              | Effect                                                                                                                      |
| ------------------- | ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| **Stop**            | card panel header                  | Kill webhook; `worker_status: killed`; uncommitted work is lost.                                                            |
| **Stop** (playbook) | playbook detail page, with confirm | Marks the run `stopped`, then kills the run's live workers on this instance; see [Playbook runs](#playbook-runs).           |
| **Stop All**        | board header, with confirm         | Kills every queued or running container in the project.                                                                     |
| Kill switch         | `config.yaml`                      | Disable or remove the task backend: the run button disappears and triggers return `503 BACKEND_DISABLED`. Restart required. |

A card claimed through another instance shows "Running on <instance>" instead
of Stop, and Stop All skips it; that instance owns the container.

## Playbook runs

A runnable playbook (see [playbooks](playbooks.md#running-a-playbook)) is
run from its detail page or by the `run_playbook` MCP tool. The server then runs its card entries one after
another: each card is launched exactly as **Run Auto** would launch it, with
`create_base_branch` set so the worker creates the playbook branch on its
first run in a repository.

| Run status  | Meaning                                                                                                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `running`   | The current card is queued or executing.                                                                                                                                       |
| `waiting`   | A human is needed: a manual step, an imported card that is not vetted, or the current card parked, stalled, failed, was killed, or could not be launched. `reason` says which. |
| `stopped`   | Stop was pressed; the **Stop** bullet below says which workers it killed.                                                                                                      |
| `completed` | Every entry is complete. The compare links open the final pull requests.                                                                                                       |

- **Run** starts a run, or resumes a `stopped` one. A resume re-launches the
  current card: a parked or stalled card is first moved back to `todo`. A card
  that finished meanwhile is skipped; one that is running is waited on. Run
  is refused while a run is `running` or `waiting`. A Run pressed while a
  Stop is still finishing waits for it.
- **Stop** kills, on this instance, the current card's worker and every
  worker queued or running on the run's other cards when the stop is written,
  skipping `done` and `not_planned` cards. A launch in flight finishes first,
  and Stop then kills the worker it queued. A hand run on another card after
  the stop is left alone.
- The runner never retries on its own. After a failed card, fix the cause,
  then press Stop and Run: Stop ends the `waiting` run and Run resumes it.
- An imported card that is not vetted holds the run at `waiting` with the reason
  `<card> not vetted`. Vetting the card is the go-ahead: the run continues by
  itself, without Run.
- While a run is `running` or `waiting`, the cards' own run buttons are
  disabled and `POST .../run` answers `409 PLAYBOOK_RUN_ACTIVE`. A hand run that races the playbook's Run either queues first,
  and the run waits on its worker, or is refused the same way.
- While any of its cards has a queued or running worker, the run starts no other card: an
  entry that turns incomplete is handled after that worker ends. The run
  points at the live card only when its current entry is unset, complete or
  gone; a run on a failed or launch-failed card keeps that entry and its
  reason while another card's worker holds it. A card left `queued` or
  `running` with no worker behind it holds the run until the reconcile sweep
  marks its worker `failed`, once the agent lists no container for the card
  and the card has not changed for 10 minutes; Stop on the card or Stop All in
  its project clears it sooner. A claim through another instance without a
  worker does not stop the run from starting other cards; when it is on the
  next card, the run waits on it until the claim goes.
- A Run after a Stop that left a worker alive (the kill failed, or the card
  is claimed through another instance), or after a hand run while stopped,
  waits on that worker before starting anything else.
- On a shared board the instance that pressed Run owns the run; other
  instances show it and never walk it. A restart resumes owned runs.

## Cost tracking

Workers call `report_usage` with prompt, completion and cache tokens, the
model, an optional provider-reported cost, and a `source`. The agent backend
is the only collector: its calls carry measured counts. Skills and interactive
callers report estimates and omit `source`. Rates come from `token_costs` or the
endpoint catalog. Where it shows:

- Card **Info** tab, "Models used": a total that includes subtasks, a cost
  bar and legend split by role, this card's models grouped under each role,
  and one block per subtask, with a marker when any bucket is estimated.
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
