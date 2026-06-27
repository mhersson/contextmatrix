# Agent backend — v1 parity

`contextmatrix-agent` is a v1-parity, operator-selectable **task** backend,
co-equal with `contextmatrix-runner`. The two coexist permanently; exactly one
serves task execution at a time (global selection). This page records the parity
audit and how to select the agent.

The runner executes cards by spawning Claude Code headless in a disposable
container, driven by the behavioral contract in `workflow-skills/` (served as MCP
prompts). The agent is a custom Go harness backed by OpenRouter that implements
the same `TaskBackend` contract with a hand-written orchestrator FSM. This audit
checks the agent against the runner — the parity bar — capability by capability.

## Selecting the agent backend

Backend selection is global (runner XOR agent), set in this server's
`config.yaml` under `backends` and read once at startup. To run the agent
backend:

1. Set `backends.agent.{url, api_key, enabled: true}` — `api_key` must be at
   least 32 characters (`MinBackendAPIKeyLength`) and match the agent's own
   configured HMAC key; `url` is the agent's public webhook listener (e.g.
   `http://localhost:9092`).
2. Set `backends.runner.enabled: false`. The runner is mutually exclusive with
   the agent: enabling both fails validation
   (`internal/config/config.go` — runner XOR agent/chat).
3. Restart ContextMatrix.

The runner remains the default; the agent is opt-in. Per-project routing is not
supported — selection is instance-wide (the per-project setting only toggles
remote execution on/off and overrides the worker image). See
`config.yaml.example` for the full block and the `CONTEXTMATRIX_*` /
`backends.*` env overrides.

## Parity matrix

Citations are `path:line`. `agent` = the `contextmatrix-agent` repo, `runner` =
the `contextmatrix-runner` repo, unprefixed = this repo (ContextMatrix).
`workflow-skills/` (this repo) is the runner's behavioral contract. A CM-dispatch
cell of `n/a — in-container` means the behavior runs inside the worker with no
backend-specific CM hook (CM delivers the card via the one trigger path);
`n/a — backend-internal` means a serve-side lifecycle concern with no CM hook.
Verdicts: **parity** / **intentional-divergence** / **gap**.

| Capability | Runner (bar) | Agent (impl) | CM dispatch | Verdict |
| --- | --- | --- | --- | --- |
| Autonomous FSM phase order | `workflow-skills/run-autonomous.md` (drives the full lifecycle) | `agent internal/orchestrator/orchestrator.go:123` `phaseOrder` (plan→execute→document→review→integrate→done) | `internal/api/runner.go:246` (autonomous forces `interactive=false`) | parity |
| classify / diagnose (bug-aware planning) | `workflow-skills/systematic-debugging.md` (create-plan Phase 0 investigation) | `agent internal/orchestrator/classify.go` (bug/maintenance gate) + `plan.go:287` `runDiagnose` | n/a — in-container | parity |
| Document phase | `workflow-skills/document-task.md` | `agent internal/orchestrator/document.go:24` `runDocument` (best-effort, between execute and review) | n/a — in-container | parity |
| Review: 3 specialists + fix loop | `workflow-skills/review-task.md` (production-ready gate, revise cycle) | `agent internal/orchestrator/review.go:350` (correctness/design/security) + `:284` `reviewRound` fix loop; read-only `agent internal/tools/readonly.go:16` `NewReadOnlyRegistry` | n/a — in-container | parity |
| HITL gates (brainstorm + plan-approval + review-decision) | `workflow-skills/create-plan.md` + `brainstorming.md` | `agent internal/orchestrator/gate.go:29` `gatePlanApproval` / `:30` `gateReviewDecision`; `gate.go:50` `gate()` (autonomous auto-passes) + `agent internal/harness/inbox.go:24` `Inbox`; brainstorm `agent internal/orchestrator/plan.go:396` | `internal/api/runner.go:254` (`Interactive` in trigger) | parity |
| Task-skill engagement (deliver / engage / report) | bind-mount + `runner internal/callback/client.go:228` `POST /api/runner/skill-engaged` | mount `agent internal/taskskills` + `agent internal/tools/skill.go:130` onEngage + `agent internal/cmclient/client.go:468` `RecordSkillEngaged` | `internal/service/service_runner.go:489` `RecordSkillEngaged` (dedups both paths) + `internal/api/runner.go:736` `getTaskSkillsSource` | parity |
| Repo grounding | Claude Code reads `CLAUDE.md`/`AGENTS.md` natively | `agent internal/orchestrator/grounding.go:34` `discoverGrounding` (built at `orchestrator.go:233` `newRun`; injected into all model phases) | n/a — in-container | parity |
| Model selection (priors-only) | sonnet/opus toggle (`internal/api/runner.go:225` `use_opus_orchestrator`) | `agent internal/registry/registry.go:188` `SelectByComplexity` + `agent internal/registry/build.go:11` `FromSelection` | `internal/api/runner.go:276` attaches `protocol.SelectionContext` (agent only) | parity (agent-specific superset) |
| Git workflow (incremental commit/push, fixup, autosquash, lease) | `workflow-skills/execute-task.md` + `review-task.md` | `agent internal/worker/git.go:448` `CommitFixup`, `:487` `RebaseAutosquash`, `:370` `ForcePushWithLease`, `:330` `guardPush` | n/a — in-container | parity |
| Promote + fail-closed verify-autonomous | `/promote` webhook calls the CM promote endpoint first (fail-closed) | `agent internal/webhook/handler.go:566` `VerifyAutonomous` before `:583` promote frame; `agent internal/callback/client.go:148` (fail-closed) | `internal/api/runner.go:385` `promoteCard` (human-only, idempotent) + `:709` `getCardAutonomous` | parity |
| Per-card budget ledger | Claude Code `--max-turns`/`--max-cost` + container limits (no CM-side ledger) | `agent internal/orchestrator/budget.go:29` `Ledger` (`Check`/`Spend`) + `agent internal/worker/worker.go:64` `MaxCardCost` (`CMX_MAX_CARD_COST`) | n/a — in-container | parity (agent-specific) |
| Crash-resume from persisted phase | `workflow-skills/run-autonomous.md` (resume from card state/phase) | `agent internal/orchestrator/orchestrator.go:298` `SetPhase` before each phase + `reconcile` re-entry | card state via MCP `update_card` (source of truth) | parity |
| Honest audit-trail attribution | backend work attributed to `runner` (`backendAuthor()` default) | agent work attributed to `agent` (`cmd/contextmatrix/main.go:311` `SetTaskBackendName`) | `internal/service/service.go:235` `backendAuthor` + `internal/service/service_runner.go` (6 sites) | parity |
| Message-dedup idempotency | `runner internal/webhook/replay_cache.go:20` `ReplayCache` + `message_dedup_cache.go:21` `MessageDedupCache` | `agent internal/webhook/replay.go:24` `ReplayCache` (wired `agent serve.go:149`/`:171`) + `agent internal/webhook/dedup.go:18` `DedupCache` (wired `agent handler.go:501`/`:539`) | n/a — backend-internal | parity |
| Container reconcile | CM sweep + internal maintenance loop (`runner internal/container/manager.go:2062` `CleanupOrphans` per tick) | `agent internal/webhook/handler.go:639` `GET /containers` + boot orphan sweep (`agent internal/executor/docker.go:477`) | `internal/runner/reconcile.go:80` `StartReconciliationSweep` (backend-agnostic) | intentional-divergence |
| `/readyz` draining | `runner internal/webhook/health.go:47` `handleReadyz` (draining → 503; also gates `!PreflightPassed` → 503) | `agent internal/webhook/handler.go:674` `handleReadyz` (draining → 503) | n/a — backend-internal | parity (draining; runner also gates preflight — see divergences) |
| Graceful shutdown | `runner cmd/contextmatrix-runner/main.go` (drain → shutdown → kill tracked → report failed) | `agent internal/cli/serve.go:230` `gracefulShutdown` | n/a — backend-internal | parity |
| Orphan cleanup (boot, label-based) | `runner internal/container/manager.go:2062` `CleanupOrphans` (label `LabelRunner=true`) | `agent internal/executor/docker.go:477` `CleanupOrphans` (label `contextmatrix.agent=true`), boot `agent internal/cli/serve.go:143` | n/a — backend-internal | parity |
| Metrics surface | `runner internal/metrics/metrics.go` (`cmr_*`, loopback admin, HMAC) | `agent internal/metrics/metrics.go:85` (`cm_agent_*`, loopback admin, HMAC; admin listener `agent internal/cli/serve.go:180`) | n/a — backend-internal | parity (subset; see divergences) |

18 of 19 capabilities are at parity; one matrix row carries the
`intentional-divergence` verdict (container reconcile); three further nuances are
recorded below. Three capabilities are also met by an
agent-specific mechanism that meets or exceeds the runner's bar (model selection,
the per-card budget ledger) or reaches the same outcome by a different route
(repo grounding).

## Intentional divergences

- **Reconcile is CM-driven.** The agent has no internal reconcile loop; CM's
  backend-agnostic sweep (`internal/runner/reconcile.go:80`) queries the agent's
  `GET /containers`, exactly as it does the runner's, and kills any container
  whose card is gone, terminal, or past its max-age. The agent adds a boot-time
  label-based orphan sweep for crash leftovers. CM is the single authority on
  whether a container should run, so the agent needs no runtime loop of its own.
- **Knowledge base retired.** Repo grounding replaced the knowledge base
  (`AGENTS.md` / `CLAUDE.md`). Claude Code reads those files natively, so the
  runner keeps grounding too; the agent walks them explicitly
  (`agent internal/orchestrator/grounding.go`). Same outcome, different mechanism.
- **Metric prefix `cm_agent_*`** (agent) vs `cmr_*` (runner) — parallel,
  namespaced surfaces on each backend's loopback admin listener. Scrape both to
  cover cards routed to either backend. The agent's series are a subset of the
  runner's; the runner's extras are feature-specific (chat, preflight) or
  explicitly deferred on the agent.
- **Readiness preflight.** The runner additionally gates `/readyz` on a preflight
  check (Docker/DNS/image availability) before it will accept work; the agent has
  no preflight stage and gates `/readyz` on draining only. Both return 503 while
  draining — this is a Kubernetes-readiness nicety, not a task-execution-parity
  gap.

## Validation

Parity was established by this code-vs-features audit, not a fresh live run: each
behavior was live-validated in its own delivery increment, and the agent has executed
real cards end-to-end (autonomous and HITL) through ContextMatrix. The audit is
the sign-off.
