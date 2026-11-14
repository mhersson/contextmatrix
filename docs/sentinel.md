# Sentinel

Sentinel is the healing controller inside ContextMatrix. It exists to run,
without a human, the loop a person runs today when an autonomous card gets
stuck: find out why, fix the card and rerun it, and only for card runs this
instance launched. It lives in `internal/sentinel` as a level-triggered
controller, built like the playbook runner. A pass advances the heals in
progress, opens heals for stopped runs and derives Sentinel's status (see
[One pass](#one-pass)). A heal that reaches `apply` writes its fix and reruns
the card (see [Applying a fix](#applying-a-fix)); one at `usage_wait` reruns
the card once the usage limit resets (see
[Usage-limit reruns](#usage-limit-reruns)). A heal whose rerun ends settles
and charges what the rerun cost (see [Settling a rerun](#settling-a-rerun)).
The status callback hands the controller run kinds' results, which it records
and charges.

## Settings

The switches, caps and threshold live in one row of the `sentinel_settings`
table in ops.db. With no row, the defaults apply. Every pass rereads the row,
so a change needs no restart. `PutSentinelSettings` validates before it writes
and records the time and the writer; invalid settings store nothing.

| Setting                   | Default | Valid range          | Read by                                                        |
| ------------------------- | ------- | -------------------- | -------------------------------------------------------------- |
| `enabled`                 | off     |                      | The controller: off reports `off` and reads nothing else       |
| `healing`                 | on      |                      | The pass: off opens no heal, dispatches nothing and reruns nothing; open heals still close, park and settle |
| `friction`                | on      |                      | The run-kind intake: off stores no findings                    |
| `daily_cap_usd`           | 20.00   | 0 to 10000           | The budget gate on the endpoint channel                        |
| `investigate_ceiling_usd` | 2.00    | above 0, at most 100 | The investigate run's `max_cost_usd`                           |
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

- **Endpoint:** open while today's Sentinel spend on the endpoint channel
  (its ledger rows since 00:00 UTC, settled heal reruns included) is below
  `daily_cap_usd`, else `daily cap reached`.
- **Claude Code:** cost does not count. Open while every stored subscription
  window whose reset time is still ahead has a status other than `rejected`
  and a utilization below `headroom`, else
  `waiting for subscription headroom`. A window whose reset time has passed
  counts as reset.

## The run row

`card_run_modes` in ops.db holds one row per card: the last run this instance
launched for it. Every launch (a UI or REST Run, MCP `run_card`, a playbook
step, Sentinel's rerun) replaces the row and records:

| Column          | Value                                                                                                     |
| --------------- | --------------------------------------------------------------------------------------------------------- |
| `mode`          | The LLM channel mode the run started in                                                                   |
| `started_at`    | The launch time, in nanoseconds                                                                           |
| `edit_snapshot` | sha256 hashes of the title, verify, autonomous flag, body intro and each H2 section (`board.TakeEditSnapshot`) |
| `autonomous`    | The card's `autonomous` flag at launch                                                                    |
| `origin`        | `rest`, `mcp`, `playbook`, or `sentinel` for Sentinel's rerun of a card outside a playbook run (inside one, the playbook step's `playbook`) |
| `attempts`      | Heal reruns, at most `MaxHealAttempts` (2): Sentinel counts one before each rerun from `apply`; a `rest` launch resets it to 0, every other launch keeps it |
| `usage_reruns`  | Usage-limit reruns, at most `MaxUsageReruns` (3): Sentinel counts one before each rerun from `usage_wait`; reset and kept like `attempts` |

A launch also clears `run_id`, `stop_cause`, `stopped_at`, `human_stop`,
`decided` and `ended_at`, which fill in as the run goes:

- `run_id` comes from the first `running` status callback that carries one,
  while none is stored and the run has not stopped.
- `stop_cause` and `stopped_at` come from the first `completed` or `failed`
  status for the stored `run_id`; a cause over 64 bytes is stored as unknown
  (empty). The callback records them before it updates the card.
- `human_stop` is set by Stop and Stop All before the card reads `killed`.
- `ended_at` is set once CM sees the card's worker end.
- `decided` is set once Sentinel has looked at the run: when it opens a heal
  for it, or decides to leave it. While `healing` is on, each pass considers
  the autonomous runs not yet decided.

`edit_snapshot` is refreshed after each write by the run's non-human claim
holder and after Sentinel writes a fix, so only human, unclaimed and other
system writes read as edits against it.

## Heal records

Three ops.db tables hold what Sentinel does. The pass opens heals, and the
run-kind intake records results, findings and spend on them.

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
- Recording never applies a result or moves the stage; the next pass acts on
  it (see [The heal flow](#the-heal-flow)).

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

A heal's rerun is an ordinary card run, so its cost is on the card. When the
rerun settles, the heal's ledger is charged one row for it (see
[Settling a rerun](#settling-a-rerun)), so on the endpoint channel heal reruns
count toward `daily_cap_usd`. A rerun still going is not counted yet.

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
on Claude Code the `windows` that have not reset. `healing` counts the heals
still open after the pass, `healed_today` the cards whose heal reached `healed`
since 00:00 UTC, and `spent_today_usd` the endpoint ledger since 00:00 UTC; a
pass reads the spend before it acts, so a charge it makes shows on the next.

## Run kinds

A run kind is a sandboxed agent run Sentinel dispatches for one card through
the agent's `POST /run-kind` ([remote execution](remote-execution.md#post-agent_urlrun-kind)).
It gets no MCP key, so it cannot write the board, and it reports only on its
terminal status (see Run-kind results). The pass dispatches one when the heal
flow says so.

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
(a newer agent's stop cause) reads as `unknown`. The pass opens each heal by
its cause's row (see [One pass](#one-pass)).

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
the violations. A new check is one function in `checks.go`'s list. The pass
runs `checkEdits` on every open heal and `vetFix` at stage `apply`; nothing
calls `vetForward` at present.

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
`apply`, the fix checks' violations. It reads no clock and does no I/O. The
pass calls it for every open heal.

A step is to wait, dispatch a run kind, advance to a stage, apply the fix,
rerun after a usage limit, park or close with a reason, or settle a rerun. The
first matching rule wins:

1. **Stage `rerun`** (the card still exists): until a run starts at or after
   the relaunch, wait, and park once 10 minutes have passed
   (`relaunchGrace`). A run that started since with an origin a rerun never
   has (a UI or REST Run, or `run_card`; a rerun's is `sentinel` or
   `playbook`) closes the heal, since a human or another run took over: it
   is neither settled nor charged. While the worker is queued or running,
   wait. A card in `done` settles `healed`. Otherwise, once the run settles
   (`causeKey`), a `done` stop settles `healed` and any other stop
   `rerun_failed`. The rerun holds the card's claim and worker, so no later
   rule applies to it. This holds for a rerun from `apply` and from
   `usage_wait` alike.
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
   to `apply`; `model` advances to `apply` holding no fix (`healFix` ignores
   any fix a diagnosis other than `card` carries); `environment` and
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

## One pass

`Controller.pass` (`internal/sentinel/pass.go`) runs on every wake-up:

1. It reads the settings. While `enabled` is off the status reads `off` and
   nothing else is read.
2. It reads the agent's effective mode, the subscription windows and today's
   spend, decides the budget gate, and reads the open heals. It lists the
   agent's containers once, and only while a run kind is in flight, and reads
   the agent's health once, and only while `healing` is on and a heal may
   dispatch. A failed listing counts every run kind as listed, so none parks
   on it; a failed health read leaves no worker slot free.
3. For every open heal it reads the card and the run row fresh, runs
   `checkEdits` against the launch snapshot and, at stage `apply`, `vetFix` on
   a `card` diagnosis's fix, so a retire-only fix that skipped the check run
   kind is still vetted. `nextStep` decides, and the pass dispatches, advances
   the stage, parks or closes. A run kind it sends takes a worker slot for the
   rest of the pass. At `apply` it writes the fix and reruns the card, at
   `usage_wait` it reruns the card to resume, and at `rerun` it settles the
   heal once the rerun ends.
4. While `healing` is on, it considers every undecided run whose card has no
   open heal (`consider.go`). A run that has not settled waits. One that is not eligible, or
   whose cause is ignored, is marked decided. Otherwise a heal opens at
   `investigate` (either investigate response) or `usage_wait`, or parked at
   once: for a `park` row with its reason, for `investigate_fix` once the card
   has used its 2 heal attempts, and for an investigation of a run with no run
   ID, which left no log.
5. It records the status.

A heal whose read or step fails is logged and decided again on the next pass.
A pass that advanced a heal, opened one, or found one written since it read it
(a run kind's status arrived) or its card written since it read the card (the
guarded fix write was refused) nudges once, so the next step is decided at
once. A pass reads only the heals still open, so a run kind's result that
arrives after its heal parked or closed is recorded and charged, never applied.

A run kind that never reported parks its heal; the same save marks the
dispatch failed, and the dispatch's ceiling is then charged. A late report
matches no dispatch, so the ceiling is the only charge.

### Activity entries

A heal writes its activity entries on its card, as `system:sentinel` with
action `sentinel`, cut to the card service's 2000-byte limit:

- when it reruns the card from `apply`, `Sentinel <what it changed>;
  rerunning fresh, attempt N of 2` (`rerunning to resume` for a resumed
  rerun), naming an updated `## Sentinel` section, a changed verify and the
  retired subtasks, or that it changed nothing on the card;
- when it reruns the card from `usage_wait`, `Sentinel rerunning to resume
  after a usage limit, usage-limit rerun N of 3`;
- when it parks, `Sentinel left this for a human: <reason>`. A rerun that is
  refused or never starts parks, so its heal writes both.

Closing or settling a heal writes none. The activity log carries only those
lines; the full reason and the rest of the heal's detail stay on its
`sentinel_heals` row.

## Applying a fix

At stage `apply` the pass decides how the card will rerun, writes the heal's
fix (`apply.go`), then reruns the card (`relaunch.go`). A heal with no route
parks before the fix is written, so the card gets only the park's activity
entry.

`applyFix` writes the fix as `system:sentinel` in one `PatchCard` guarded on
the card's `updated` as the pass read it: the `## Sentinel` section (section
upsert) when the fix has one, the verify fields when it has them, and a cleared
`phase` for a fresh rerun. A human edit in between refuses the write
(`ErrCardChanged`); the heal stays at `apply` and the next pass decides again,
parking it when the edit check finds the edit. Each subtask the fix retires
then moves to `not_planned`, one guarded write per subtask on a fresh read, so
a claim that lands first wins; a refused retire parks the heal with
`cannot retire <ID>: <class>` (see [Refusals](#refusals)). Last, the run row's
`edit_snapshot` is replaced with the written card's, so the edit check reads
Sentinel's write as no edit. A heal holding no fix (a `model` diagnosis)
writes nothing. The heal records the change: the section and verify before
and after, the retired subtasks, and fresh or resume.

A rerun is fresh, its saved phase cleared so the agent plans again, when the
fix wrote the section or retired subtasks; otherwise it resumes, keeping the
agent's recorded escalation and sizing.

`relaunchRoute` picks how the card reruns, from the run row's origin and the
playbook the card's active `playbook_lock` names. On either route the card
first moves to `todo` when it is elsewhere:

| Origin           | Route                                                                                                                                         |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `playbook`       | Through the playbook run's Stop then Play, as `system:sentinel`, while that run is active on this instance and its current entry is the card's, so Stop kills no other card's worker; else it parks |
| any other origin | Alone: the card launches as a hand run with origin `sentinel`; while an active playbook run holds the card it parks                          |

The playbook route needs the move too: a resumed run's walker relaunches its
card only from `todo` or after a failed, killed, parked or stalled run, and a
run whose `completed` status arrived without a park leaves the card with no
worker status.

Before anything launches, `relaunch` counts the rerun on the run row, saves
the heal at `rerun` with `relaunched_at` and the card's cost with its
subtasks' as the cost base, and writes the rerun's activity entry, so the
rerun's run row starts at or after `relaunched_at`. A move to `todo`, Stop or
Play that fails parks the heal with `rerun refused: cannot <step>: <class>`,
and a refused launch with `rerun refused: <launch error>`. The launch is
`backendHandlers.sentinelRelaunch` (`internal/api/backend_runkind.go`), which
the router hands the controller: a hand run's checks and `launch`, with a raw
service error reduced to its sanitized class.

### Refusals

A park reason is committed with the card, so a refused write's raw error,
which can carry paths and remote URLs, is only logged (`refused.go`). The
reason names the step and one class, decided from the error:

| Class                           | Errors                                                                                                   |
| ------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `the project does not allow it` | A refused transition, no transition path, or a state the project lacks                                   |
| `it changed meanwhile`          | A guarded write that lost, a claim, a deleted card, or a playbook run stopped, started or made unrunnable since the pass read it |
| `refused`                       | Anything else                                                                                            |

## Usage-limit reruns

A run a Claude Code usage limit stopped opens its heal at `usage_wait`. Once
the limit resets (`park_resets_at`; a card without one does not wait) and
while the [budget gate](#budget-gate) is open, so on Claude Code while every
subscription window is below `headroom`, the pass reruns the card through the
same route and steps as a fix's rerun (`usageRerun` in `relaunch.go`), with
two differences: nothing is written to the card, so the agent resumes at its
saved phase, and the rerun counts `usage_reruns` instead of a heal attempt.
A card that has used its 3 usage-limit reruns parks instead.

## Settling a rerun

Once a heal's rerun ends (stage `rerun`, rule 1 of
[the heal flow](#the-heal-flow); a run a human or `run_card` started since
closes the heal instead), `settle` (`settle.go`) saves the heal at
`healed` or `rerun_failed`, then charges the ledger one row for the rerun:
the card's cost with its subtasks' now, less the cost base taken at the
relaunch, never below zero (`rerunCharge`), on the mode the rerun's run row
records. The stage is saved first, so a save refused because the heal changed
since the pass read it charges nothing, and the next pass settles and charges
once.

A rerun's run row is a new run, which stays undecided while its heal is open.
Once the heal settles, the same pass considers that run like any other while
`healing` is on (see [One pass](#one-pass)): a failed rerun opens the card's
next heal, parked at once when the card has used its 2 heal attempts.

## Where things live

| File                                        | Holds                                                                                                                                                        |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/sentinel/settings.go`             | `Settings`, the defaults and validation                                                                                                                      |
| `internal/sentinel/state.go`                | The durable-state types: the run row (`Run`, `LaunchFacts`), launch origins, the caps, and the heal records (`Heal`, the stages, `Finding`, `RunKindReport`) |
| `internal/sentinel/controller.go`           | The loop: `Start`, `Shutdown`, `Nudge`, `Status`, the panic-safe pass and the wake-up filter; the `Store`, `Backend`, `Cards`, `Playbooks` and `PlaybookRunner` it uses |
| `internal/sentinel/payload.go`              | The pure run-kind payloads: `investigatePayload` under the 1 MiB cap, `checkPayload`, and `siblings`                                                         |
| `internal/sentinel/dispatch.go`             | Reading a run kind's inputs, `dispatch` (record, then send, then act on the agent's answer) and the no-run-kinds hold                                         |
| `internal/sentinel/outcome.go`              | The ends of a heal: `park` (with its activity entry, `activityText`), `parkUnreported` (and the ceiling charge) and `closeHeal`                              |
| `internal/sentinel/apply.go`                | Applying a fix: the route first, then `applyFix` (the guarded write, retiring subtasks, the snapshot refresh) and `freshRerun`                              |
| `internal/sentinel/relaunch.go`             | Rerunning a card: `planRelaunch` and the pure `relaunchRoute`, `usageRerun`, `relaunch` (counting the rerun, rerun stage, activity entry, then the move to `todo` and launch or Stop then Play) and the activity texts |
| `internal/sentinel/settle.go`               | Settling a rerun: `settle` (the stage, then the charge) and the pure `rerunCharge`                                                                           |
| `internal/sentinel/refused.go`              | Refused writes: `refusal`, the class a park reason names, and `refused`, which logs the raw error                                                            |
| `internal/sentinel/intake.go`               | `RunKindStatus`: record a run kind's terminal status, its windows and findings                                                                               |
| `internal/sentinel/limits.go`               | `ValidateRunKindStatus`: the run-kind status values and size limits                                                                                          |
| `internal/sentinel/spend.go`                | `Pricer` and `priceUsage`: what a run kind's usage costs                                                                                                     |
| `internal/sentinel/pass.go`                 | One pass: read the gate's inputs and the open heals, build each heal's view and act on its step, consider undecided runs, record the status                 |
| `internal/sentinel/consider.go`             | Considering undecided runs: open a heal for a settled, eligible run, parked at once or at its stage, or mark the run decided                                 |
| `internal/sentinel/causes.go`               | The cause table: one row per cause, `responseFor`                                                                                                            |
| `internal/sentinel/checks.go`               | `fixContext`, the two check lists, `vetFix` and `vetForward`                                                                                                 |
| `internal/sentinel/check_*.go`              | One check per file: shape, section, retire, edits, verify, text                                                                                                            |
| `internal/sentinel/trigger.go`              | The pure triggers: `causeKey` (settled and cause), `eligible`, and `openStage` (the stage a new heal opens at)                                               |
| `internal/sentinel/heal.go`                 | The pure heal flow: `nextStep` over a `healView`, one rule per stage and exit; `healFix`                                                                     |
| `internal/sentinel/gate.go`                 | The pure budget gate, the daily cap's day (`dayStart`) and the worker-slot check (`roomFor`)                                                                 |
| `internal/sentinel/status.go`               | `Status`, the states and the pure summary                                                                                                                    |
| `internal/opstore/sqlite/sentinel.go`       | The settings row and the subscription windows the controller reads                                                                                           |
| `internal/opstore/sqlite/card_runs.go`      | The run row                                                                                                                                                  |
| `internal/opstore/sqlite/sentinel_heals.go` | Heals, findings and the spend ledger                                                                                                                         |
| `internal/api/backend_runkind.go`           | `runKindBase`: a run kind's common payload fields and its contents-read token; `sentinelRelaunch`: Sentinel's launch of a rerun                            |

Tests sit next to each file.
