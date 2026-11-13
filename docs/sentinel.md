# Sentinel

Sentinel is the healing controller inside ContextMatrix. It exists to run,
without a human, the loop a person runs today when an autonomous card gets
stuck: find out why, fix the card and rerun it, and only for card runs this
instance launched. It lives in `internal/sentinel` as a level-triggered
controller, built like the playbook runner. At present a pass reads Sentinel's
settings, the agent's LLM channel mode and the stored subscription windows,
and derives Sentinel's status from them; it does not act on runs.

## Settings

The switches, caps and threshold live in one row of the `sentinel_settings`
table in ops.db. With no row, the defaults apply. Every pass rereads the row,
so a change needs no restart. `PutSentinelSettings` validates before it writes
and records the time and the writer; invalid settings store nothing.

| Setting                   | Default | Valid range          | Read by                                                        |
| ------------------------- | ------- | -------------------- | -------------------------------------------------------------- |
| `enabled`                 | off     |                      | The controller: off reports `off` and reads nothing else       |
| `healing`                 | on      |                      | Stored; nothing reads it at present                            |
| `friction`                | on      |                      | Stored; nothing reads it at present                            |
| `daily_cap_usd`           | 20.00   | 0 to 10000           | The budget gate on the endpoint channel                        |
| `investigate_ceiling_usd` | 2.00    | above 0, at most 100 | Stored; nothing reads it at present                            |
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

Three ops.db tables hold what Sentinel does; nothing writes them at present.

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
| `paused`      | `daily cap reached` or `waiting for subscription headroom` | The budget gate is closed                                             |
| `working`     |                                                            | At least one heal is in progress                                      |
| `watching`    |                                                            | Otherwise                                                             |

The status also carries `enabled`, `mode` (the agent's effective mode, empty
while Sentinel is off or has no controller), `daily_cap_usd`, `headroom`, and
on Claude Code the `windows` that have not reset. `healing`, `healed_today` and
`spent_today_usd` are 0 at present, so the state is never `working`.

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
fix's resolved verify (a fix verify with no content, such as an empty env list, is stored as none, so the card inherits the project's). Command fields are compared by whitespace-separated
fields, so an unchanged command needs no candidate, and a changed one must match
a candidate. The timeout may change freely; env names may only be removed. The text
check ignores the card's own `## Sentinel` section, so text Sentinel wrote
earlier never whitelists itself.

## Where things live

| File                                        | Holds                                                                                                                                                        |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/sentinel/settings.go`             | `Settings`, the defaults and validation                                                                                                                      |
| `internal/sentinel/state.go`                | The durable-state types: the run row (`Run`, `LaunchFacts`), launch origins, the caps, and the heal records (`Heal`, the stages, `Finding`, `RunKindReport`) |
| `internal/sentinel/controller.go`           | The loop: `Start`, `Shutdown`, `Nudge`, `Status`, the panic-safe pass and the wake-up filter                                                                 |
| `internal/sentinel/pass.go`                 | One pass: read settings, mode and windows; record the status and publish a change                                                                            |
| `internal/sentinel/causes.go`               | The cause table: one row per cause, `responseFor`                                                                                                            |
| `internal/sentinel/checks.go`               | `fixContext`, the two check lists, `vetFix` and `vetForward`                                                                                                 |
| `internal/sentinel/check_*.go`              | One check per file: shape, section, retire, edits, verify, text                                                                                                            |
| `internal/sentinel/trigger.go`              | The pure triggers: `causeKey` (settled and cause), `eligible`                                                                                                |
| `internal/sentinel/gate.go`                 | The pure budget gate and the worker-slot check (`roomFor`)                                                                                                   |
| `internal/sentinel/status.go`               | `Status`, the states and the pure summary                                                                                                                    |
| `internal/opstore/sqlite/sentinel.go`       | The settings row and the subscription windows the controller reads                                                                                           |
| `internal/opstore/sqlite/card_runs.go`      | The run row                                                                                                                                                  |
| `internal/opstore/sqlite/sentinel_heals.go` | Heals, findings and the spend ledger                                                                                                                         |

Tests sit next to each file.
