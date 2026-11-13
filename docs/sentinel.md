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
  mode it ran on.

A run kind's terminal status is recorded on the card's newest heal with that
kind in flight and not yet answered or failed, whatever the heal's stage, and
charges one ledger row: its usage, or the dispatch's ceiling when a model kind
reported none. A repeat of the status, or one with no such heal, records and
charges nothing. Recording never moves the stage.

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

## Where things live

| File                                        | Holds                                                                                                                                                        |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/sentinel/settings.go`             | `Settings`, the defaults and validation                                                                                                                      |
| `internal/sentinel/state.go`                | The durable-state types: the run row (`Run`, `LaunchFacts`), launch origins, the caps, and the heal records (`Heal`, the stages, `Finding`, `RunKindReport`) |
| `internal/sentinel/controller.go`           | The loop: `Start`, `Shutdown`, `Nudge`, `Status`, the panic-safe pass and the wake-up filter                                                                 |
| `internal/sentinel/pass.go`                 | One pass: read settings, mode and windows; record the status and publish a change                                                                            |
| `internal/sentinel/gate.go`                 | The pure budget gate and the worker-slot check (`roomFor`)                                                                                                   |
| `internal/sentinel/status.go`               | `Status`, the states and the pure summary                                                                                                                    |
| `internal/opstore/sqlite/sentinel.go`       | The settings row and the subscription windows the controller reads                                                                                           |
| `internal/opstore/sqlite/card_runs.go`      | The run row                                                                                                                                                  |
| `internal/opstore/sqlite/sentinel_heals.go` | Heals, findings and the spend ledger                                                                                                                         |

Tests sit next to each file.
