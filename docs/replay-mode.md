# Replay Mode (Design)

This is a design document for a feature that **does not exist** in the codebase.
Will consider building it when one of the conditions in
[When to build this](#when-to-build-this) is concretely met. Speculative
infrastructure has a maintenance cost even when it isn't built.

## When to build this

Build replay when one of these is concretely true:

1. A regression class emerges that the stub suite cannot catch, and reproducing
   it requires a multi-actor MCP sequence (parent + sub-agents, realistic
   interleaving) too painful to script as stub directives.
2. Production capture (Path B below) is being built for a separate reason —
   observability, replay-of-bugs — and replay machinery is on the path.
3. A model evaluation initiative needs deterministic re-execution of recorded
   runs across model versions.

Do not build replay because:

- "It would be nice to have."
- "We feel like we should have workflow-correctness coverage."
- The integration suite feels too small.

## What replay is

Replay records the agent's actions during a real run as a manifest, then plays
those actions back into a fresh CM + runner + fixture. The system under test
runs live; only the agent (Claude) is faked.

Replay is **not** an eval — it does not measure agent capability. The recording
captures one trajectory; replay enforces it. If the recording was made on an
artificial fixture, replay enforces the artificial shape.

## Two paths to manifests

### Path A — harness-recorded

Run a real-Claude scenario in a local harness (re-introduced as needed). The
harness produces `worker.raw.jsonl` and a fixture bare repo. A recorder tool
processes those into a manifest.

Pros: no new infrastructure, privacy-clean, reproducible. Cons: manifests
inherit the harness fixture's artificiality; each manifest costs one real-Claude
harness run.

### Path B — production-recorded

Production runs hit real codebases and tasks. CM is in k8s, runner is in a
separate VM, so the capture/upload path needs to be built.

**Capture (on the runner VM).** The runner already attaches to worker container
stdout for its logparser. Extend it to tee the stream to
`/var/lib/contextmatrix-runner/recordings/<run_id>/worker.raw.jsonl`. On each
`report_push` notification, the runner does `git fetch <branch>` from the real
remote and snapshots the tip tree to `pushes/<n>.tar`.

**Upload (runner → CM, existing HTTPS channel).** On success, the runner POSTs
the bundle to a new `POST /api/internal/recordings/<run_id>` endpoint on CM. The
k8s/VM split is a non-issue — they already talk over the network. Bundle size:
5-50MB stream-json + 1-10MB per push.

**Ingest (CM-side).** CM stores the bundle in object storage and records
metadata in the DB
(`run_id, project, card_id, model, recorded_at, status, retention_until`).
Auto-expire after 90 days unless a manifest has been extracted.

**Opt-in.** Per-project flag `record_for_replay: true` in `.board.yaml`, default
false. Initial allowlist: only projects we own (harness, canary, dogfood).
Customer projects need a scrubbing pass first — see Privacy below.

**Manifest extraction.** `cmd/replay-record --from-recording <run_id>` pulls a
bundle from CM and produces a manifest. Manifests get committed to
`test/integration/replay/manifests/` after a human review.

## Manifest format

```json
{
  "scenario": "autonomous-canary",
  "model": "claude-sonnet-4-6",
  "recorded_at": "2026-04-15T12:34:56Z",
  "schema_version": 1,
  "events": [
    { "t": "mcp", "name": "claim_card", "args": { "card": "$parent" } },
    {
      "t": "mcp",
      "name": "create_card",
      "args": { "parent": "$parent", "title": "Server" },
      "alias": "$sub1"
    },
    {
      "t": "git_push",
      "branch": "feat/sysinfo",
      "tree": "trees/event_27.tar",
      "msg": "feat: server"
    },
    {
      "t": "mcp",
      "name": "report_usage",
      "args": { "card": "$parent", "prompt_tokens": 152334 }
    },
    { "t": "operator_msg", "content": "approve" },
    { "t": "mcp", "name": "complete_task", "args": { "card": "$parent" } }
  ]
}
```

**Aliases** (`$parent`, `$sub1`): bound at record time by **creation order**. CM
mints fresh IDs at replay time; the replay-worker keeps a runtime alias→ID map.
Every tool call that takes a card ID is remapped through the table.

**Schema versioning.** `schema_version` lets old manifests be detected and
rejected. Bump on any non-backward-compatible change.

**Not in events.** Heartbeats are emitted by the replay-worker on a fixed
cadence in the background, not from the manifest. Recording each heartbeat would
bloat the manifest for no signal.

## Components

### Recorder (`cmd/replay-record`)

Standalone tool, two modes:

- `--from-harness <diagnostics-dir>`: read harness scenario output
  (`worker.raw.jsonl`, bare fixture repo, `cards.json`). Path A.
- `--from-recording <run_id>`: pull a production bundle from CM. Path B.

Outputs: `test/integration/replay/manifests/<scenario>.json` plus tree archives
under `<scenario>/trees/<n>.tar`.

Walk the stream-json envelopes, extract `tool_use` content blocks, map each to a
manifest event. For each `git_push` event run `git archive` against the recorded
branch tip. Bind aliases by creation order. Produce stable, sorted output for
clean diffs across re-recordings.

### Replay-worker (`test/integration/replay-worker/`)

A sibling of the stub-worker pattern (re-introduced as needed). Same Dockerfile
shape, same MCP client, manifest-driven `main.go`.

Lifecycle:

1. Mount the manifest, connect to CM via MCP using the standard auth flow.
2. Walk events:
   - `mcp`: substitute aliases → IDs, call the tool. Bind the alias if the event
     has one.
   - `git_push`: extract the tree tarball,
     `git add -A && git commit -m <msg> && git push <fresh-fixture-url> <branch>`.
     Recorded URL is discarded.
   - `operator_msg`: skip — these are emitted by the test side via
     `messageCard`, not the worker.
3. Send synthetic heartbeats on a configurable cadence in the background.
4. Exit when the manifest exhausts or `complete_task(parent)` fires.

Pacing: clamp inter-event delays to ≤100ms. A 15-minute recorded run should
replay in under a minute.

### Test wiring

- `bootReplayScenario(t, manifestPath)` mirroring the existing `bootScenario`
  pattern but pointing the runner at `cm-replay-worker:test`.
- Replay scenarios in `scenarios_test.go` gated by manifest presence (skip if
  missing).
- The test side reads the same manifest in parallel to emit `operator_msg`
  events at the recorded ordering points.

## Hard parts

**ID rewriting.** CM generates `INT-1`, `INT-2`, etc. in creation order. Replay
must create cards in the same order — the alias table substitutes the fresh IDs
in every tool call that references one.

**Git state.** Tree archives (`git archive`) lose commit hashes/timestamps but
are simpler. Pack snapshots preserve history exactly but are harder to wire.
Default to tree archives unless an assertion needs commit-hash stability.

**HITL gates.** No real text frames in replay. Operator messages are recorded
directly as `operator_msg` events; the test side emits them via `messageCard` at
the recorded ordering point.

**Multi-actor runs.** Real-Claude spawns sub-agents; recorded `worker.raw.jsonl`
mixes parent and sub-agent calls. Recommended first cut: serialise everything
into one MCP session and accept that multi-actor identity assertions don't apply
to replay. Multi-actor identity is a behavioural property; it belongs in
real-Claude tests if anywhere.

**Token-usage values.** `report_usage` calls in the recording have real numbers.
Replay sends them verbatim. Don't synthesise zero.

## What replay catches

- CM regressions: ID assignment, transition rules, activity log, MCP tool
  wiring, auth, dedup logic.
- Runner regressions: webhook protocol, container lifecycle, log parsing, gate
  handling.
- Fixture / git plumbing: push handling, branch creation.
- Outcome assertions on the resulting state: pushed branch contents, parent card
  body sections, activity log entries.

## What replay does NOT catch

- Changes in **Claude's behaviour**. A model upgrade, a skill rewrite, a prompt
  edit — invisible to replay.
- New MCP tool semantics. If a tool's signature changes, recorded calls become
  invalid. The replay should fail and we re-record.

## Privacy (Path B specific)

Production worker stream-json may contain customer code, private repo URLs, and
credentials if the agent misbehaved.

Mitigations:

- Per-project opt-in (`record_for_replay: true`), default false.
- Initial allowlist: projects we own only.
- Before extending to customer projects, design and ship a scrubbing pass:
  remove anything matching credential patterns, anything flagged by the secrets
  store, any path containing a customer identifier. This is its own design
  effort, not part of the initial replay build.

## Manifest staleness

Manifests recorded against `claude-sonnet-4-6` say nothing about later models.

- Stamp `model` and `recorded_at` in every manifest.
- Replay refuses to run on `schema_version` mismatch.
- Warn (don't fail) if a manifest is older than 90 days. Re-record cadence is
  operator-driven.

## Starting checklist

If you're picking this up because a build condition fired:

1. Confirm the build condition with whoever asked. If it's "we'd like to have
   it," go back and re-read the intro.
2. Decide Path A or Path B based on what the build is for. Path A is much
   smaller; build it first if unsure.
3. Read `test/integration/scenario_test.go`, the existing stub-worker under
   `test/integration/stub-worker/`, and the runner's logparser to ground
   yourself in the existing contracts.
4. Write the manifest format and a single hand-crafted manifest. Get the
   round-trip working with a stub replay-worker before investing in a recorder.
5. Then write the recorder. Then add real scenarios.
