# Integration Tests

`test/integration/` runs end-to-end harness scenarios against real CM and runner
binaries with a synthetic stub worker. Two entry points share the `integration`
build tag:

- `TestIntegrationHarness` (`scenarios_test.go`) drives seven scenarios —
  Autonomous, HITL, KillMidRun, HeartbeatTimeout, PromoteHITLToAuto,
  IdleWatchdog, Chat. Each exercises the CM↔runner↔worker lifecycle under
  deterministic conditions; `STUB-DIRECTIVE` comments in card bodies inject
  specific worker behaviours (e.g. `hang-after-claim`, `skip-heartbeat`). Its
  shared CM config pins `auth.mode: none` (`writeCMConfig` in
  `config_test.go`): these scenarios enable `backends.runner`, and under the
  config default (`auth.mode: multi`) an enabled runner backend is a startup
  error — the runner backend is deprecate-frozen under multi-user auth — so
  the pin is required for CM to boot at all.
- `TestMultiUserAdminSurface` (`multiuser_test.go`) is a standalone scenario
  that boots its own admin-surface-only CM instance in `auth.mode: multi`,
  with no task backend configured, and exercises the auth + admin HTTP
  surface end to end over real HTTP: unauthenticated rejection,
  bootstrap-token redemption, password login, a non-admin user's 401/403
  contract, and a GitHub credential create + project binding (validated
  against a local fake GitHub server). It needs no Docker worker, but still
  runs under the `integration` tag because `TestMain` builds the runner
  binary and stub image unconditionally for the whole package.

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
