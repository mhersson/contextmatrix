# Integration Tests

`test/integration/` runs end-to-end harness scenarios against real CM and runner
binaries. Two suites:

- **Stub** (`make test-integration`, ~25s): six scenarios driven by a synthetic
  worker (`test/integration/stub-worker/`) that speaks MCP but fakes Claude.
  Covers heartbeat-timeout, idle-watchdog, kill-mid-run, promote-HITL-to-auto,
  and the autonomous + HITL happy paths.
- **Real-Claude** (`make test-integration-real`, ~28min, ~$0.80 per run): builds
  the worker Docker image, sends a canary card to the runner, and asserts that
  real Claude (Sonnet sub-agents, Opus reviewer) drives the card to `done`
  through the full production workflow. PR creation is intentionally skipped
  (the fixture is a self-signed HTTPS git server, not a GitHub remote).

Per-scenario diagnostics land at `/tmp/cm-int-runs/<scenario>-<ts>/`:
`cards.json`, `cm.log`, `runner.log`, `combined.log`, `worker.raw.jsonl`,
`transcript.jsonl`, and a generated `run.md` summary.

## Scope and caveats

The real-Claude suite reliably catches **plumbing** regressions:

- Worker workspace mount, MCP token auth, container env scrubbing.
- `git clone` / `git push` against the fixture HTTPS server.
- `report_push` wiring, sub-agent dispatch (at least one), agent-claim/release.
- Generated server compiles, tests pass, serves valid JSON (`assertCanaryServer`
  is the strongest assertion — it does `go build`, `go test`, then hits `GET /`
  against the live binary).
- harness-canary-skill mount path **in autonomous mode**.

It does **not** reliably catch **workflow-correctness** regressions after the
relaxations applied during the iteration loop:

- Plan decomposition collapse — assertion is `≥1` subtask, not `≥2`.
- HITL gate emission/dialogue — the responder spam-`approve`s on buffer-idle,
  doesn't parse gate prompts.
- Phase 4 (Documentation) — no direct assertion.
- Review findings semantic content — header-only string match.
- HITL harness-canary-skill mount — assertion dropped (orchestrator sometimes
  processes subtasks inline, never invoking the Skill tool).
- Heartbeat scheduling — proxied via `report_usage`, not asserted directly.

**Treat a passing run as evidence the plumbing works, not as evidence the
production workflow is correct.** Do not gate every PR on this suite. Manual /
nightly / pre-release only.

## Follow-ups

Items below tighten the harness toward what the spec promised. Categorised by
priority.

### Required before relying on this as a workflow-correctness gate

- [ ] **Restore `≥2` subtask threshold OR add explicit Phase 3 fan-out signal.**
      Currently `≥1` hides regressions that collapse Phase 3 into a single
      inline subtask. Counting `Agent` tool calls with `subagent_type` derived
      from `get_skill('execute-task', ...)` would be a direct fan-out signal.
  - File: `test/integration/scenarios_test.go`, `assertAuthenticAutonomousRun`
    step 2.
- [ ] **Re-introduce harness-canary-skill assertion for HITL.** The orchestrator
      skips sub-agent spawning in HITL, so the Skill tool is never invoked.
      Either fix the orchestrator and restore the assertion, or move the
      assertion to a per-subtask check that fails when an execute-task sub-agent
      doesn't engage the skill.
  - File: `test/integration/scenarios_test.go`, `testHITLRealClaude`.
- [ ] **Replace idle-pump HITL responder with real gate parsing.** The current
      8-second-after-text auto-approve fires before gates exist; the agent
      treats early `approve` as preemptive consent, then ignores subsequent
      gates. Need to parse the runner's gate marker (or the orchestrator's
      specific question prompt structure) and only fire in response. The
      `approvalsSent ≥ 2` assertion currently means "the agent paused for 8s
      twice" — it has no relation to gate count.
  - File: `test/integration/scenario_test.go`, `startHITLGateResponder`.
- [ ] **Tighten plan-section check.** `## Plan OR ## Subtasks` is decorative —
      the runner already creates `## Subtasks` from `create_card` calls, so the
      OR-fallback is satisfied without Phase 1 having run. Either require
      `## Plan` with N lines of body, or assert on an explicit `update_card`
      activity-log entry that mutated the body during Phase 1.
  - File: `test/integration/scenarios_test.go`, `assertAuthenticAutonomousRun`
    step 1.

### Should fix

- [ ] **Phase 4 (Documentation) signal.** Currently inferred from "Phase 5 ran"
      — protocol doesn't actually guarantee that. Require an `add_log` action
      like `documentation_written` from the doc agent, or assert the runner
      emitted `skill_engaged: documentation` on the parent.
- [ ] **Review-findings semantic check.** A non-empty `## Review Findings`
      section with a recognisable `recommendation:` line, not just a header
      match.
- [ ] **Direct heartbeat assertion.** Count `mcp__contextmatrix__heartbeat` tool
      calls in `worker.raw.jsonl` (or activity-log proxy) and require `≥3` for a
      real-Claude run. The current `report_usage > 0` proxy silently drops if a
      future skill change unbundles heartbeats.
- [ ] **Multi-actor agent identity assertions.** Verify `pushed` action's
      `agent` field is the orchestrator (not a sub-agent), and that sub-agents
      are distinct from the orchestrator on `complete_task`. A regression that
      collapses the multi-actor model would be invisible today.
- [ ] **Token-usage threshold beyond `> 0`.** A sub-agent that ToolSearched and
      bailed has `prompt_tokens > 0`. Real execution costs thousands. A
      threshold of `≥1000` would catch no-op sub-agents without false-positiving
      on cheap tasks.
- [ ] **Stop instructing the agent to engage harness-canary-skill in the card
      body.** The current body says "Before you touch any code, invoke the Skill
      tool with skill=harness-canary-skill" — this tests instruction-following,
      not skill resolution. Let the skill's description trigger engagement
      organically (the SYSINFO-CANARY marker in the body should match the skill
      description).
  - File: `test/integration/scenarios_test.go`, `canaryCardBody`.
- [ ] **Panic-resilient cleanup.** When `go test -timeout` panics, `t.Cleanup`
      doesn't fire — `cards.json` and `run.md` are never written, and the
      operator gets nothing in the worst-case scenario. Use
      `signal.NotifyContext` in `TestMain` and run cleanups explicitly on
      signal, or write `cards.json` periodically (every N seconds) so the latest
      snapshot survives.
  - File: `test/integration/main_test.go`, `test/integration/scenario_test.go`.
- [ ] **Update the spec.**
      `docs/superpowers/specs/2026-05-04-real-claude-authentic-workflow-design.md`
      sets stronger guarantees than the implementation now delivers
      (specifically items #2 ≥2 subtasks and #8 skill mount in HITL). Either
      revise the spec to match, or fix the implementation to match the spec.
- [ ] **Drop unused parameter on `dockerListByScenario`.** Today the function
      ignores its `scenarioID` arg and relies on subtests running sequentially.
      First parallelisation will sweep sibling-scenario containers. Either
      delete the parameter or actually filter by it.
  - File: `test/integration/scenario_test.go`.

### Nice-to-have

- [ ] **Replay mode.** Record a real-Claude transcript once, replay it through
      the stub for routine CI runs. Spec listed this as out of scope; cost makes
      it attractive.
- [ ] **Rename / split the HITL test.** What it currently tests is "auto-approve
      flood doesn't crash the run." If the responder isn't upgraded to parse
      gates, name it accordingly to avoid implying it exercises HITL gate
      semantics.
- [ ] **Worker stream-json sanity.** `summarizeWorkerStream` silently skips bad
      lines. If a regression made the runner's logparser drop a large fraction
      of frames, the test wouldn't notice — and the assertions don't check tool
      counts at all. A floor (e.g. ≥10 `mcp__` tool calls) would catch silent
      log-parsing regressions.
- [ ] **Tool-count assertions.** Add a per-run check on the worker's MCP tool
      usage: e.g. `claim_card ≥ 4` (parent + sub-agents), `report_usage ≥ 5`
      (orchestrator + each phase), `heartbeat ≥ 3`. Cheap to assert; catches
      workflow shape changes.

### Infrastructure / process

- [ ] **Add `README.md` to `test/integration/` describing the scope narrowing**
      (this file's "Scope and caveats" section, abbreviated). First-time readers
      shouldn't have to follow a docs link to learn the suite isn't a
      workflow-correctness gate.
- [ ] **CI wiring.** Spec leaves CI out of scope. Decide: nightly run on `main`?
      Manual trigger? Pre-release gate? Whichever it is, wire it explicitly and
      document in the runbook so passing runs aren't silently used as PR-merge
      evidence.
- [ ] **Cost telemetry.** Each run prints token usage to `cards.json`. Aggregate
      per-run cost into a CI metric so cost regressions (skill ballooning,
      infinite-loop respawns) surface as a build signal.
