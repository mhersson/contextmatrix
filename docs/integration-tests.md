# Integration Tests

`test/integration/` runs end-to-end harness scenarios against real CM and runner
binaries with a synthetic stub worker. Six scenarios: Autonomous, HITL,
KillMidRun, HeartbeatTimeout, PromoteHITLToAuto, IdleWatchdog. Each exercises
the CM↔runner↔worker lifecycle under deterministic conditions; `STUB-DIRECTIVE`
comments in card bodies inject specific worker behaviours (`hang-after-claim`,
`skip-heartbeat`).

Run: `make test-integration` (~70s including stub image build, requires Docker).

Per-scenario diagnostics land at `$TMPDIR/cm-int-runs/<scenario>-<ts>/` (macOS)
or `/tmp/cm-int-runs/<scenario>-<ts>/` (Linux): `cards.json`, `cm.log`,
`runner.log`, `combined.log`, `worker.raw.jsonl`, `transcript.jsonl`, `run.md`.

## Scope

The suite covers integration concerns that unit tests can't:

- CM↔runner webhook protocol and auth.
- MCP tool round-trips via the runner-spawned worker container.
- Worker container lifecycle (spawn, kill on `/stop`, idle-watchdog termination,
  heartbeat-timeout cleanup).
- Card state transitions driven by worker actions (`claim → in_progress → done`;
  heartbeat-timeout → `stalled`).
- HITL chat-loop wiring (chat message → worker stdin → response).

It does NOT cover real Claude reasoning, planning, skill engagement, or
multi-actor workflow correctness. Those are not currently tested at the
integration level — see "Future work" below.

## Future work

If we want behavioural signal from a real model, the cheapest first step is a
single manually-triggered smoke test that asserts a card reaches `done` and the
artifact compiles — no relaxed graders, no skill assertions. Not currently
implemented.

For the more ambitious replay-mode direction (recording real runs and replaying
them deterministically against fresh CM/runner), see
[`replay-mode.md`](replay-mode.md). That doc explicitly frames replay as
build-only-when-needed; do not build it speculatively.
