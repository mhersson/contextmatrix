# Integration Tests

`test/integration/` runs end-to-end harness scenarios against real CM and runner
binaries with a synthetic stub worker. Seven scenarios driven from
`TestIntegrationHarness` in `scenarios_test.go`: Autonomous, HITL, KillMidRun,
HeartbeatTimeout, PromoteHITLToAuto, IdleWatchdog, Chat. Each exercises the
CM↔runner↔worker lifecycle under deterministic conditions; `STUB-DIRECTIVE`
comments in card bodies inject specific worker behaviours (e.g.
`hang-after-claim`, `skip-heartbeat`).

Run: `make test-integration` (~70s including stub image build, requires Docker).
The Make target runs
`go test -tags=integration -count=1 -timeout 15m ./test/integration/...`.

Harness prerequisites (asserted by `main_test.go` at startup, the suite fails
fast otherwise):

- The `contextmatrix-runner` source repo must be checked out at
  `../contextmatrix-runner` relative to this repo. The harness builds the runner
  binary from that source tree into a temp directory; it does not use a
  system-installed runner.
- A Docker daemon reachable from the current user. The harness builds the
  `cm-stub-legacy:test` image from `test/integration/stub-worker/` on first run
  (cached thereafter) and spawns worker containers labelled with the scenario ID
  for cleanup.

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
  heartbeat-timeout → `stalled`; promote flipping the autonomous flag).
- HITL chat-loop wiring (chat message → worker stdin → response).
- Global-chat REST surface end-to-end (`Chat` scenario): create / get / list /
  patch / delete against a live SQLite store, with the expected HTTP status
  codes. Sending a message and the reopen flow are not wired through the
  stub-worker — see comments in `testChatStub`.

It does NOT cover real Claude reasoning, planning, skill engagement, or
multi-actor workflow correctness. Those are not currently tested at the
integration level — see "Future work" below.

## Future work

The cheapest first step toward behavioural signal from a real model is a single
manually-triggered smoke test that asserts a card reaches `done` and the
artifact compiles — no relaxed graders, no skill assertions. Not currently
implemented.

For the more ambitious replay-mode direction (recording real runs and replaying
them deterministically against fresh CM/runner), see
[`replay-mode.md`](replay-mode.md). That doc explicitly frames replay as
build-only-when-needed; do not build it speculatively.
