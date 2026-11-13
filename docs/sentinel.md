# Sentinel

Sentinel is the healing controller inside ContextMatrix. It exists to run,
without a human, the loop a person runs today when an autonomous card gets
stuck: find out why, fix the card and rerun it, and only for card runs this
instance launched. It lives in `internal/sentinel` as a level-triggered
controller, built like the playbook runner. At present a pass reads Sentinel's
settings, the agent's LLM channel mode and the stored subscription windows,
and derives Sentinel's status from them; it does not act on runs. The status
callback hands the controller run kinds' results, which it records and charges.

## Settings

The switches, caps and threshold live in one row of the `sentinel_settings`
table in ops.db. With no row, the defaults apply. Every pass rereads the row,
so a change needs no restart. `PutSentinelSettings` validates before it writes
and records the time and the writer; invalid settings store nothing.

| Setting                   | Default | Valid range          | Read by                                                        |
| ------------------------- | ------- | -------------------- | -------------------------------------------------------------- |
| `enabled`                 | off     |                      | The controller: off reports `off` and reads nothing else       |
| `healing`                 | on      |                      | Stored; nothing reads it at present                            |
| `friction`                | on      |                      | The run-kind intake: off stores no findings                    |
| `daily_cap_usd`           | 20.00   | 0 to 10000           | The budget gate on the endpoint channel                        |
| `investigate_ceiling_usd` | 2.00    | above 0, at most 100 | The investigate run's `max_cost_usd`; no pass dispatches one at present |
| `headroom`                | 0.85    | above 0, at most 1   | The budget gate on Claude Code, as a utilization fraction      |

Numbers must be finite.

## The controller

- `main.go` builds the controller only when an agent backend is configured,
  starts it after the router, and joins it in shutdown phase 4 with the
  playbook walkers.
- One goroutine runs a pass at start, then one per wake-up: the 30-second
  tick, a bus event of type `worker.*`, `card.stalled`, `card.updated`,
  `card.state_changed`, `llm_channel.changed` or `playbook.updated`, or
  `Controller.Nudge`. Nudges that arrive during a pass coalesce into one.
- Level-triggered: a pass decides from durable state, never from the event
  that woke it. Wake-ups only shorten the wait, so a dropped event or a
  restart is covered by the next pass.
- A pass that panics is logged with its stack and recovered; a pass whose read
  fails is logged. Either leaves the status as it was, and the next wake-up
  tries again.
- When the status a pass derives differs from the last one published, the
  controller publishes `sentinel.changed` with an empty project and no data.

### Budget gate

The gate uses the agent's effective LLM channel mode: the current mode, or
`endpoint` when the agent does not support `claude_code`.

- **Endpoint:** open while today's Sentinel spend is below `daily_cap_usd`,
  else `daily cap reached`. Sentinel dispatches no paid work, so its spend is
  0.
- **Claude Code:** cost does not count. Open while every stored subscription
  window whose reset time is still ahead has a status other than `rejected`
  and a utilization below `headroom`, else
  `waiting for subscription headroom`. A window whose reset time has passed
  counts as reset.

## The run row

`card_run_modes` in ops.db holds one row per card: the last run this instance
launched for it. Every launch (a UI or REST Run, MCP `run_card`, a playbook
step) replaces the row and records:

| Column          | Value                                                                                                     |
| --------------- | --------------------------------------------------------------------------------------------------------- |
| `mode`          | The LLM channel mode the run started in                                                                   |
| `started_at`    | The launch time, in nanoseconds                                                                           |
| `edit_snapshot` | sha256 hashes of the title, verify, autonomous flag, body intro and each H2 section (`board.TakeEditSnapshot`) |
| `autonomous`    | The card's `autonomous` flag at launch                                                                    |
| `origin`        | `rest`, `mcp` or `playbook`; `sentinel` is defined for Sentinel's own reruns, and no launch uses it at present |
| `attempts`      | Heal reruns, at most `MaxHealAttempts` (2): a `rest` launch resets it to 0, every other launch keeps it   |
| `usage_reruns`  | Usage-limit reruns, at most `MaxUsageReruns` (3): reset and kept like `attempts`                          |

Nothing increments `attempts` or `usage_reruns` at present. A launch also
clears `run_id`, `stop_cause`, `stopped_at`, `human_stop`, `decided` and
`ended_at`, which fill in as the run goes:

- `run_id` comes from the first `running` status callback that carries one,
  while none is stored and the run has not stopped.
- `stop_cause` and `stopped_at` come from the first `completed` or `failed`
  status for the stored `run_id`; a cause over 64 bytes is stored as unknown
  (empty). The callback records them before it updates the card.
- `human_stop` is set by Stop and Stop All before the card reads `killed`.
- `ended_at` is set once CM sees the card's worker end.
- `decided` is set once Sentinel has looked at the run: when it opens a heal
  for it, or decides to leave it. The store lists the autonomous runs not yet
  decided; nothing sets it at present.

`edit_snapshot` is refreshed after each write by the run's non-human claim
holder, so only human, unclaimed and system writes read as edits against it.

## Heal records

Three ops.db tables hold what Sentinel does. Nothing opens a heal at present;
the run-kind intake records results, findings and spend on one that exists.

- `sentinel_heals`: one row per heal, for one stopped run (at most one per run
  start, so opening it twice returns the first), or per fix forwarded to
  another card. Opening a heal marks its run decided in the same transaction.
  The row holds the stage, the run kind in flight and its answer, the
  investigation and check results, the subtasks and siblings sent, and the
  change made to the card. `healed`, `rerun_failed`, `parked` and `closed` end
  a heal; `forward_passed` and `forward_failed` end a forward row.
- `sentinel_findings`: the friction findings an investigation reported.
- `sentinel_spend`: the spend ledger, one row per charge with the LLM channel
  mode it ran on and, for a run kind's status, its run ID.

A run kind's terminal status is recorded on the card's newest heal with that
kind in flight and not yet answered or failed, whatever the heal's stage, and
charges one ledger row: its usage, or the dispatch's ceiling when a model kind
reported none. A status whose run ID is already on the ledger is a repeat, so
it records and charges nothing even when an older heal of the card still has
such a dispatch open; so does one with no such heal. Recording never moves the
stage.

Every write to a heal row bumps its version, and a save carries the version it
read: one made from a stale read is refused (`ErrHealChanged`), so a pass never
overwrites an answer recorded since it read the heal.

## Run-kind results

A run kind reports once, on its terminal status, through the status callback
(`POST /api/agent/status` with `kind` set). The callback routes on `kind`
before it touches the card, so the card and its run row never see the status.

- `ValidateRunKindStatus` (`limits.go`) checks every value and size limit
  (listed in the [API reference](api-reference.md#post-apiagentstatus)). A breach is 422 and nothing is recorded.
- `Controller.RunKindStatus` (`intake.go`) records a valid status on the
  card's heal as above: the kind's result, or without one the status message,
  else its stop cause, else its worker status, as the dispatch's failure.
- Once a status is recorded, and only then, it records the usage rows'
  subscription windows (see Spend), stores the investigation's findings while
  `friction` is on, publishes `sentinel.changed` with the card's project and
  ID, and nudges the controller.
- A store failure is returned, so the callback answers 500 and the backend's
  retry delivers the status again. A failure after recording is logged
  instead: the retry would be a repeat.
- Recording never applies a result; nothing acts on one at present.

## Spend

Each recorded run-kind status charges the heal's ledger one row on the LLM
channel mode its dispatch ran on, once per run ID (`spend.go`):

- Each usage row is charged its gateway cost (`cost_usd`) when the gateway
  reported one, else the estimate from CM's rate table and model catalog
  (`CardService.PriceTokens`), the same rule card runs and chats use. A row
  priced neither way charges nothing.
- An `investigate` status with no usage is charged the dispatch's ceiling, so
  the daily cap never under-counts a run that died before it reported. A
  `check` runs no model.
- A Claude Code row's `subscription` windows are recorded as `report_usage`
  records them, so the budget gate reads current windows.
- Nothing reaches the card's own token usage or cost.

## Status

`Controller.Status` summarizes the last pass as of the moment it is asked, so
a controller whose passes stopped landing reads as not running.
`NotRunningStatus` is the status of a server with no controller. The first
matching state wins:

| State         | Reason                                                     | When                                                                  |
| ------------- | ---------------------------------------------------------- | --------------------------------------------------------------------- |
| `off`         |                                                            | `enabled` is off                                                      |
| `not_running` | `no execution backend`                                     | Sentinel is on and no agent backend is configured, so no controller exists |
| `not_running` | `controller stopped`                                       | No pass has landed for more than three ticks                          |
| `paused`      | `the agent has no run kinds`                               | The agent answered a run kind with 404 in the last 15 minutes         |
| `paused`      | `daily cap reached` or `waiting for subscription headroom` | The budget gate is closed                                             |
| `working`     |                                                            | At least one heal is in progress                                      |
| `watching`    |                                                            | Otherwise                                                             |

The status also carries `enabled`, `mode` (the agent's effective mode, empty
while Sentinel is off or has no controller), `daily_cap_usd`, `headroom`, and
on Claude Code the `windows` that have not reset. `healing`, `healed_today` and
`spent_today_usd` are 0 at present, so the state is never `working`.

## Run kinds

A run kind is a sandboxed agent run Sentinel dispatches for one card through
the agent's `POST /run-kind` ([remote execution](remote-execution.md#post-agent_urlrun-kind)).
It gets no MCP key, so it cannot write the board, and it reports only on its
terminal status (see Run-kind results). No pass dispatches one at present.

Every run kind's payload starts from the same base, filled by
`backendHandlers.runKindBase` (`internal/api/backend_runkind.go`), which the
router hands the controller:

- the project's repository and worker image, and the card's base branch;
- a git token for the repository with contents read only
  (`GenerateRepoReadToken`), minted from the project's credential binding and
  failing closed as a launch does. A PAT cannot be narrowed, so a PAT install
  sends the PAT. The token carries no expiry: the run clones once;
- for `investigate`, the model, selection context and LLM endpoint a card
  launched now would get (`chooseModel`, shared with `launch`), and the mode
  that endpoint belongs to. `check` runs no model and gets none of them.

| Kind          | Sends                                                                                                                                                                                                                       |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `investigate` | `max_cost_usd` = `investigate_ceiling_usd`; the stopped run's ID and stop cause (none for `stalled` and `unknown`); the card file; its verify resolved card over project; its subtasks' card files; its unstarted siblings' card files |
| `check`       | A trial of the verify the fix leaves, resolved as the card will store it (none when the fix keeps verify); the card's resolved verify command in force today; the fix's `## Sentinel` text                                    |

Siblings are the cards of the playbook run holding the card, when that run is
active on this instance: card entries in the same project that are incomplete,
in `todo` with no worker status, other than the card itself, in entry order.

The agent refuses a body over 1 MiB, so the investigate payload is filled in
order: the card, then subtasks, then siblings, while the encoded payload stays
at or under 1 MiB. The first file that does not fit ends both lists. The heal
records the subtask and sibling IDs sent, since a fix may retire only a
subtask the investigation saw. A card whose file alone does not fit is not
dispatched and the heal parks.

`dispatch` (`dispatch.go`) records the dispatch on the heal (kind, mode,
ceiling, time) before it sends, so a run the agent starts is always on
record. A save refused because the heal changed since it was read sends
nothing. Only a definite answer that started nothing clears the dispatch:

| Agent answer                       | Effect                                                                                                    |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------- |
| 404, the agent has no run kinds    | The dispatch is cleared, and nothing is dispatched for 15 minutes; the status reads paused meanwhile      |
| 429 or 503, busy                   | The dispatch is cleared; a later pass retries                                                             |
| Another 4xx, refused               | The dispatch is cleared and the heal parks                                                                |
| A transport error or other failure | The agent may have admitted the run, so the dispatch stays on record and the error is logged: it is answered when the run reports, or the settle grace parks it with its ceiling charged. Clearing it could lose the run's spend and send a second run |

A run kind takes one of the agent's `max_concurrent` worker slots and must
leave one free for card runs: `roomFor` (`gate.go`) allows a dispatch only
while `running_containers + 1 < max_concurrent`.

## The cause table

`causeTable` in `internal/sentinel/causes.go` maps each cause to a response.
A new cause is one row in `causes.go` plus its test row. A key the table lacks
(a newer agent's stop cause) reads as `unknown`. Nothing acts on the table at
present.

| Cause                                                                                                   | Response           | Park reason                                         |
| ------------------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------- |
| `toolchain_missing`, `verify_parked`, `empty_subtask`, `max_turns`, `context_limit`, `review_parked`    | `investigate_fix`  |                                                     |
| `review_cap`                                                                                            | `park`             | `review rounds used up`                             |
| `usage_limit`                                                                                           | `usage_rerun`      |                                                     |
| `forward_fix_failed`                                                                                    | `park`             | `a fix Sentinel forwarded to this card did not hold` |
| `budget_exceeded`                                                                                       | `park`             | `budget exceeded`                                   |
| `no_model`                                                                                              | `park`             | `no model available`                                |
| `split_overflow`                                                                                        | `park`             | `the split overflowed`                              |
| `gates_parked`, `error`, `shutdown`, `timeout`, `infrastructure`, `killed`, `ended`, `stalled`, `unknown` | `investigate_park` |                                                     |
| `done`, `human_stop`                                                                                    | `ignore`           |                                                     |

`stalled`, `unknown`, `human_stop`, `review_cap` and `forward_fix_failed` are
CM's own keys; the rest are the stop causes the backend reports.

### Triggers

`causeKey` (`internal/sentinel/trigger.go`) names a run's cause once it has
settled:

- A stalled card is `stalled`.
- A card whose worker is queued or running is not settled.
- Otherwise the run is settled when its terminal status arrived (`stopped_at`),
  or when the worker is `failed`, `killed` or `parked` and the card has gone
  unwritten for the settle grace (10 minutes, `backend.StaleWorkerGrace`). The
  agent parks a card before its container exits, so a parked card waits for the
  callback's stop cause.
- A human Stop is `human_stop`. `review_parked` on a card with 7 or more
  review attempts (`board.MaxReviewAttempts`) is `review_cap`. Any stop but
  `done` or `usage_limit` on a card whose forwarded fix is still on trial and was forwarded
  before the run started is `forward_fix_failed`. An empty cause is `unknown`;
  otherwise the cause is the stop cause.

`eligible` says whether Sentinel may act on a settled run: the run was launched
autonomous, and the card is not finished, does not need vetting, is not
claimed, and is not claimed by another instance.

## The fix checks

A fix is vetted by plain functions in `internal/sentinel`, each taking a
`fixContext` and returning its violations (none means it passes). `vetFix`
runs `healChecks` and `vetForward` runs `forwardChecks`, and each concatenates
the violations. A new check is one function in `checks.go`'s list. Nothing
calls them at present.

| Check          | Refuses                                                                                                  | Lists                |
| -------------- | -------------------------------------------------------------------------------------------------------- | -------------------- |
| `checkShape`   | A fix that sets no field, or retires an ID the investigation was not sent                                | heal, forward        |
| `checkSection` | A `## Sentinel` section with an H2 line or an unbalanced code fence (``` or ~~~)                         | heal, forward        |
| `checkRetire`  | Retiring a subtask that is not this card's, is not in `todo`, is claimed, or has a queued or running worker | heal              |
| `checkEdits`   | Any change since launch: `title edited`, `verify edited`, `autonomous changed`, `intro edited`, `section "X" edited`, `added` or `removed`; no launch snapshot is also refused | heal |
| `checkVerify`  | A verify change that adds an env name (`adds env NAME`), drops the command (`removes the verify command`), changes the command to one the check run kind did not list as a candidate (`command not declared by the repo`), was never trial-run (`verify not trial-run`), or whose trial did not finish (`trial run: <trial>`) | heal, forward |
| `checkText`    | A fix with a `## Sentinel` section when the check run kind did not run (`text not checked`), or when it reports text absent from the repo that is also absent from the card's title and body (`introduces <entry>`) | heal, forward |

The verify rule compares the card's resolved verify (card over project) with the
fix's resolved verify (a fix verify with no content, such as an empty env list, is stored as none, so the card inherits the project's). Commands are compared line by line (blank lines ignored, a newline is significant), each line by whitespace-separated
fields, so an unchanged command needs no candidate, and a changed one must match
a candidate. The timeout may change freely; env names may only be removed. The text
check ignores the card's own `## Sentinel` section, so text Sentinel wrote
earlier never whitelists itself.

## The heal flow

`nextStep` (`internal/sentinel/heal.go`) decides one open heal's next step
from what a pass read: the heal, the card, the card's run row, the `healing`
switch, the budget gate, whether a worker slot is free, whether the in-flight
run kind's container is listed, the edit check's violations and, at stage
`apply`, the fix checks' violations. It reads no clock and does no I/O. No
pass calls it at present.

A step is to wait, dispatch a run kind, advance to a stage, apply the fix,
rerun after a usage limit, park or close with a reason, or settle a rerun. The
first matching rule wins:

1. **Stage `rerun`** (the card still exists): until a run starts at or after
   the relaunch, wait, and park once 10 minutes have passed
   (`relaunchGrace`). While the worker is queued or running, wait. A card in
   `done` settles `healed`. Otherwise, once the run settles (`causeKey`), a
   `done` stop settles `healed` and any other stop `rerun_failed`. The rerun
   holds the card's claim and worker, so no later rule applies to it.
2. The card is deleted, `done` or `not_planned`: close.
3. A worker is queued or running, the card is claimed, or the run row is a
   newer run than the one the heal follows: close, since a human or another
   run took over.
4. The edit check found changes since launch: park, naming them.
5. A run kind is in flight (sent, neither answered nor failed): wait while its
   container is listed or for the settle grace (10 minutes) after it was
   sent; then park, with the dispatch's ceiling marked to be charged.
6. `healing` is off: wait.
7. **Stage `investigate`:** dispatch `investigate` while the budget gate is
   open and a worker slot is free, else wait. An answer without a result
   parks with the failure the intake recorded. A cause whose response is
   `investigate_park` parks with the diagnosis. Otherwise a `card` diagnosis
   with a fix advances to `check` when the fix has a section or verify, else
   to `apply`; `model` advances to `apply` with no fix; `environment` and
   `ecosystem` park with the diagnosis; anything else parks.
8. **Stage `check`:** dispatch `check` while a worker slot is free; it runs no
   model, so the budget gate does not hold it. An answer without a result
   parks with its failure; a result advances to `apply`.
9. **Stage `apply`:** park when the fix checks refuse the fix or the card has
   used its 2 heal attempts; wait while the budget gate is shut; else apply.
10. **Stage `usage_wait`:** park when the card has used its 3 usage-limit
    reruns; wait until the usage limit resets (`park_resets_at`) and while the
    budget gate is shut; else rerun.

Rules 7 to 10 match only their own stage; past rule 6, a heal at any other
stage waits.

## Where things live

| File                                        | Holds                                                                                                                                                        |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/sentinel/settings.go`             | `Settings`, the defaults and validation                                                                                                                      |
| `internal/sentinel/state.go`                | The durable-state types: the run row (`Run`, `LaunchFacts`), launch origins, the caps, and the heal records (`Heal`, the stages, `Finding`, `RunKindReport`) |
| `internal/sentinel/controller.go`           | The loop: `Start`, `Shutdown`, `Nudge`, `Status`, the panic-safe pass and the wake-up filter; the `Store`, `Backend`, `Cards` and `Playbooks` it uses      |
| `internal/sentinel/payload.go`              | The pure run-kind payloads: `investigatePayload` under the 1 MiB cap, `checkPayload`, and `siblings`                                                         |
| `internal/sentinel/dispatch.go`             | Reading a run kind's inputs, `dispatch` (record, then send, then act on the agent's answer) and the no-run-kinds hold                                         |
| `internal/sentinel/outcome.go`              | `park`                                                                                                                                                       |
| `internal/sentinel/intake.go`               | `RunKindStatus`: record a run kind's terminal status, its windows and findings                                                                               |
| `internal/sentinel/limits.go`               | `ValidateRunKindStatus`: the run-kind status values and size limits                                                                                          |
| `internal/sentinel/spend.go`                | `Pricer` and `priceUsage`: what a run kind's usage costs                                                                                                     |
| `internal/sentinel/pass.go`                 | One pass: read settings, mode and windows; record the status and publish a change                                                                            |
| `internal/sentinel/causes.go`               | The cause table: one row per cause, `responseFor`                                                                                                            |
| `internal/sentinel/checks.go`               | `fixContext`, the two check lists, `vetFix` and `vetForward`                                                                                                 |
| `internal/sentinel/check_*.go`              | One check per file: shape, section, retire, edits, verify, text                                                                                                            |
| `internal/sentinel/trigger.go`              | The pure triggers: `causeKey` (settled and cause), `eligible`                                                                                                |
| `internal/sentinel/heal.go`                 | The pure heal flow: `nextStep` over a `healView`, one rule per stage and exit                                                                                |
| `internal/sentinel/gate.go`                 | The pure budget gate and the worker-slot check (`roomFor`)                                                                                                   |
| `internal/sentinel/status.go`               | `Status`, the states and the pure summary                                                                                                                    |
| `internal/opstore/sqlite/sentinel.go`       | The settings row and the subscription windows the controller reads                                                                                           |
| `internal/opstore/sqlite/card_runs.go`      | The run row                                                                                                                                                  |
| `internal/opstore/sqlite/sentinel_heals.go` | Heals, findings and the spend ledger                                                                                                                         |
| `internal/api/backend_runkind.go`           | `runKindBase`: a run kind's common payload fields and its contents-read token                                                                                |

Tests sit next to each file.
