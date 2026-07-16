# Integration Tests

`test/integration/` is a thin smoke harness that boots the real ContextMatrix
binary in `auth.mode: multi` and drives a small set of end-to-end scenarios.
Everything shares the `integration` build tag, so `make test` ignores it; run it
with `make test-integration` (requires Docker).

```
make test-integration
# go test -tags=integration -count=1 -timeout 20m ./test/integration/...
```

## Scenarios

| Test | Needs | What it covers |
| --- | --- | --- |
| `TestSmoke` | nothing | build-tag wiring |
| `TestMultiUserAdminSurface` | CM only | auth + admin HTTP surface: unauthenticated rejection, bootstrap-token redemption, password login, the non-admin 401/403 contract, credential create (against a local fake GitHub) + project binding |
| `TestChatREST` | CM only | chat REST CRUD (create/get/list/patch/delete) against a live SQLite store, no backend |
| `TestAgentScenario` | agent repo + Docker | the `contextmatrix-agent` backend runs an autonomous card to `done` against a scripted LLM and a seeded git server: the card is decomposed, worked, reviewed, integrated, and its `cm/int-001` branch is pushed |
| `TestChatScenario` | chat repo + Docker | the `contextmatrix-chat` backend answers one chat message from the scripted LLM; the reply must arrive on the transcript |

The runlog unit tests (`TestSummarize*`, `TestRender*`) also run under the tag.

## What `TestMain` builds

`TestMain` builds **only** the CM binary (with the embedded frontend). The two
sibling backends and their worker images are built **lazily**, guarded by
`sync.Once`, the first time a scenario needs them:

- `ensureAgentAssets` / `ensureChatAssets` compile the sibling's single binary
  twice — once as a host `serve` binary, once statically (`CGO_ENABLED=0
  GOOS=linux`) for the container — then `docker build` a **minimal**
  `cm-agent-worker:test` / `cm-chat-worker:test` image (`debian:bookworm-slim` +
  `git` + `ca-certificates` + `bash` + the binary, mirroring the production
  worker `ENTRYPOINT`). The production `docker/Dockerfile.worker` bakes in a full
  language toolchain; the smoke scenarios script a trivial change and a `true`
  verify gate, so none of that is needed.
- If a sibling repo is absent (resolved by walking up from the harness root to
  find `contextmatrix-agent` / `contextmatrix-chat`), the scenario `t.Skip`s
  with a clear message. So `TestMultiUserAdminSurface`, `TestChatREST`, and
  `TestSmoke` run with no sibling checkout present.

## Prerequisites

- A Docker daemon reachable from the current user (for the two backend
  scenarios). **The daemon must support bridge networking** — the agent and chat
  executors launch every worker on the bridge with an `--add-host
  host.docker.internal:host-gateway` mapping and expose no host-network knob. A
  daemon that cannot set up bridge networking makes worker containers
  unlaunchable; the two container scenarios then `t.Skip` with that reason. The
  fix is host/daemon-level — a common Linux cause is a missing `veth` module
  (`sudo modprobe veth`, persisted via `/etc/modules-load.d/veth.conf`), which
  may instead require a reboot when a kernel upgrade has removed the running
  kernel's module tree.
- For `TestAgentScenario` / `TestChatScenario`: the `contextmatrix-agent` /
  `contextmatrix-chat` source repos checked out next to this one, at a
  revision compatible with CM's pinned `contextmatrix-protocol` version (see
  `go.mod`). The harness builds them as-is; it does not use installed binaries.

## Auth bootstrap

CM runs in `auth.mode: multi`, so essentially the whole API is session-gated.
Each scenario bootstraps an admin session once (`bootAdminSession` in
`auth_test.go`): it scrapes the one-time bootstrap link from CM's startup logs,
redeems it to create the first admin, and returns a cookie-jar client. Scenario
clients drive everything through that session — an `X-Agent-ID` header alone does
not authenticate browser routes in multi mode.

## The scripting model

Worker containers reach three host services via `host.docker.internal`:

- **Scripted LLM** (`stubllm_test.go`) — an OpenAI-compatible endpoint on
  `0.0.0.0:<port>` serving `POST /chat/completions` as SSE. It matches on request
  **content** (the orchestrator's phase persona preambles, e.g. `"You are the
  planning agent"`) and returns the SSE body that phase expects. The matcher
  table and the SSE wire builders are ported verbatim from the agent repo's
  `internal/worker/e2e_orchestrator_test.go` `scriptedBackend` — they are
  `_test.go`-internal there and cannot be imported. Every reply carries a scripted
  `usage` cost so the `report_usage` / cost plumbing is exercised. There are two
  scripts: the agent happy path (plan → two coder subtasks → review approve →
  document → integrate) and a single canned chat reply.

  > **Matcher-sync warning.** The matchers key on the agent's phase prompts. If a
  > phase prompt is reworded upstream, update the matcher in `stubllm_test.go` in
  > lockstep or the scenario hangs on the `UNEXPECTED PROMPT` fallback. All
  > matchers live in that one file.

- **Git server** (`gitserver_test.go`) — smart-HTTP via `git http-backend` (CGI)
  on `0.0.0.0:<port>`, serving one seeded bare repo (`README` + a trivial
  `go.mod`/`main.go`) with anonymous clone and push. The board's legacy singular
  `repo:` field points at `http://host.docker.internal:<port>/work.git`. The
  agent scenario asserts the pushed branch with `git ls-remote` from the host.

- **ContextMatrix MCP** — the worker claims the card, heartbeats, and reports
  over MCP at `container_contextmatrix_url + /mcp`, Bearer-authed with the
  configured `mcp_api_key`.

CM itself cannot resolve `host.docker.internal` (the host has no such alias), so
its own catalog / chat-picker fetch of `llm_endpoint` fails — that is
best-effort and fail-open by design. Only the containers reach the endpoint.

## Runlog artifacts

Each scenario writes a per-run directory under `${TMPDIR}/cm-int-runs/<id>-<ts>/`:

- `combined.log` — chronological merge of backend/transcript/harness lines
- `cm.log`, `agent.log`, `chat.log` — subprocess stderr (only the started ones)
- `stubllm.log` — every scripted-LLM request
- `worker.raw.jsonl` — the worker container's stdout (`docker logs -f`)
- `transcript.jsonl` — the `/api/worker/logs` SSE stream (agent scenario)
- `cards.json` + `run.md` — a card-state snapshot and a rendered summary

The path is printed at the end of each scenario (`scenario diagnostics: …`).

## Running one scenario

```bash
go test -tags=integration -run TestAgentScenario -v ./test/integration/...
go test -tags=integration -run TestMultiUserAdminSurface -v ./test/integration/...
```

The harness is hermetic: every run uses fresh temp dirs and a per-run
bootstrap, and an orphan sweep (by the `contextmatrix.agent=true` /
`contextmatrix.chat=true` container labels) clears any containers a crashed run
left behind, so the suite is safe to run back to back.
