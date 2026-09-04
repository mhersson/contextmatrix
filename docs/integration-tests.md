# Integration Tests

`test/integration/` is a thin smoke harness that boots the real ContextMatrix
binary in `auth.mode: multi` and drives a small set of end-to-end scenarios.
Everything carries the `integration` build tag, so `make test` ignores it.

```bash
make test-integration
# go test -tags=integration -count=1 -timeout 20m ./test/integration/...
```

## Scenarios

| Test                        | Needs               | What it covers                                                                                                                                                                                                          |
| --------------------------- | ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestSmoke`                 | nothing             | build-tag wiring                                                                                                                                                                                                        |
| `TestMultiUserAdminSurface` | CM only             | unauthenticated rejection, bootstrap-token inspection and redemption, password login, the non-admin 401/403 contract, credential create against a local fake GitHub, project binding                                    |
| `TestChatREST`              | CM only             | chat REST create/get/list/patch/delete against a live SQLite store, no backend                                                                                                                                          |
| `TestAgentScenario`         | agent repo + Docker | the `contextmatrix-agent` backend runs an autonomous card to `done` against a scripted LLM and a seeded git server; the card is planned, coded, reviewed, documented, integrated, and its `cm/int-001` branch is pushed |
| `TestChatScenario`          | chat repo + Docker  | the `contextmatrix-chat` backend answers one chat message from the scripted LLM; the reply must land on the transcript                                                                                                  |

The runlog unit tests (`TestSummarize*`, `TestRender*`) run under the same tag.

## What `TestMain` builds

`TestMain` builds only the CM binary. When `web/dist/index.html` is missing it
first runs `make install-frontend` (if `web/node_modules` is absent) and
`make build-frontend`, because the binary embeds the frontend. It then sweeps
orphaned worker containers by label.

The two backends and their worker images are built lazily, once per process
(`sync.Once`), the first time a scenario needs them:

- `ensureAgentAssets` / `ensureChatAssets` compile the sibling's binary twice:
  once as a host `serve` binary, once statically (`CGO_ENABLED=0 GOOS=linux`)
  for the container. They then `docker build` a minimal
  `cm-agent-worker:test` / `cm-chat-worker:test` image: `debian:bookworm-slim`
  plus `bash`, `git`, `ca-certificates`, and the binary, with the production
  `ENTRYPOINT` (`contextmatrix-{agent,chat} work`). The production
  `docker/Dockerfile.worker` images carry a full language toolchain; the
  scenarios script a trivial change and a `true` verify gate, so none of that
  is needed.
- Sibling repos are resolved by walking up from the harness root looking for
  a `contextmatrix-agent` / `contextmatrix-chat` directory. When one is
  absent the scenario `t.Skip`s with a clear message, so
  `TestMultiUserAdminSurface`, `TestChatREST`, and `TestSmoke` run with no
  sibling checkout.

## Prerequisites

- **Docker** reachable from the current user, for the two backend scenarios.
  The daemon must support bridge networking: the executors launch every
  worker on the bridge with `host.docker.internal:host-gateway` and expose no
  host-network knob. When a probe container fails to start with a networking
  error, both container scenarios `t.Skip` with that reason. The fix is
  host-level; a common Linux cause is a missing `veth` module
  (`sudo modprobe veth`, persisted via `/etc/modules-load.d/veth.conf`), which
  may need a reboot when a kernel upgrade removed the running kernel's module
  tree.
- **Sibling checkouts** of `contextmatrix-agent` / `contextmatrix-chat` next
  to this repo, at a revision compatible with CM's pinned
  `contextmatrix-protocol` version (see `go.mod`). The harness builds them
  as-is and never uses installed binaries.

## Auth bootstrap

CM runs in `auth.mode: multi`, so nearly the whole API is session-gated. Each
scenario bootstraps an admin session once (`bootAdminSession` in
`auth_test.go`): it scrapes the one-time bootstrap link from CM's startup log,
redeems it to create the first admin, and returns a cookie-jar client. Every
scenario call goes through that session; an `X-Agent-ID` header alone does not
authenticate browser routes in multi mode.

## The scripting model

Worker containers reach three host services via `host.docker.internal`:

- **Scripted LLM** (`stubllm_test.go`): an OpenAI-compatible endpoint on
  `0.0.0.0:<port>` serving `POST /chat/completions` as SSE. It matches on
  request content (the orchestrator's phase persona preambles, such as
  `"You are the planning agent"`) and returns the SSE body that phase
  expects. The matcher table and SSE builders mirror the agent repo's
  `internal/worker/e2e_orchestrator_test.go` scripted backend, which is
  `_test`-internal there and cannot be imported. Every reply carries a
  scripted `usage` cost so the `report_usage` and cost plumbing is exercised.
  Two scripts exist: the agent happy path (two-subtask plan, two coders,
  first-round review approval, document, integrate) and one canned chat
  reply.

  > **Matcher-sync warning.** The matchers key on the agent's phase prompts.
  > If a phase prompt is reworded upstream, update the matcher in
  > `stubllm_test.go` in lockstep or the scenario hangs on the
  > `UNEXPECTED PROMPT` fallback. All matchers live in that one file.

- **Git server** (`gitserver_test.go`): smart HTTP via `git http-backend`
  through `net/http/cgi` on `0.0.0.0:<port>`, serving one seeded bare repo
  (`README` plus a trivial `go.mod`/`main.go`) with anonymous clone and push.
  The board's singular `repo:` field points at
  `http://host.docker.internal:<port>/work.git`; that field is not
  scheme-validated, unlike `repos[]`. The agent scenario asserts the pushed
  branch with `git ls-remote` from the host.

- **ContextMatrix MCP**: the worker claims the card, heartbeats, and reports
  over MCP at `container_contextmatrix_url` + `/mcp`, Bearer-authed with the
  configured `mcp_api_key`.

CM itself cannot resolve `host.docker.internal` (the host has no such alias),
so its own catalog and chat-picker fetch of `llm_endpoint` fails. That fetch is
best-effort and fail-open by design; only the containers reach the endpoint.

## Runlog artifacts

Each scenario writes a per-run directory under
`$TMPDIR/cm-int-runs/<scenario>-<timestamp>/`:

| File                              | Contents                                                      |
| --------------------------------- | ------------------------------------------------------------- |
| `combined.log`                    | chronological merge of backend, transcript, and harness lines |
| `cm.log`, `agent.log`, `chat.log` | subprocess stderr (only the processes that started)           |
| `stubllm.log`                     | every scripted-LLM request                                    |
| `worker.raw.jsonl`                | the worker container's stdout (`docker logs -f`)              |
| `transcript.jsonl`                | the `/api/worker/logs` SSE stream (agent scenario)            |
| `cards.json`, `run.md`            | card-state snapshot and rendered summary                      |

The path is logged at the end of each scenario as `scenario diagnostics: ...`.

## Running one scenario

```bash
go test -tags=integration -run TestAgentScenario -v ./test/integration/...
go test -tags=integration -run TestMultiUserAdminSurface -v ./test/integration/...
```

The harness is hermetic: every run uses fresh temp dirs and its own bootstrap,
and the orphan sweep (by the `contextmatrix.agent=true` /
`contextmatrix.chat=true` container labels) clears containers a crashed run
left behind, so the suite is safe to run back to back.
