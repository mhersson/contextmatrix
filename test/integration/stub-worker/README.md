# stub-worker

Stub fake-claude image for the contextmatrix integration test harness
(FSM-orchestrated path).

The image mirrors cm-orchestrated:test's shape — long-lived container
with `sleep infinity` entrypoint, `tini` as PID 1, `bash` and `git`
available — but `/usr/local/bin/claude` is replaced by a small Go
binary that fakes Claude Code's CLI surface and emits canned
stream-json markers per phase.

## How the runner uses it

1. Runner receives `/trigger`, allocates a container from
   `cm-stub-orchestrated:test`, sets env vars (CM_CARD_ID, CM_PROJECT,
   CM_MCP_URL, CM_MCP_API_KEY, CM_INTERACTIVE).
2. Container runs `tini → entrypoint.sh → sleep infinity`.
3. Runner's FSM enters Planning. `RunPlanPhaseAction` calls
   `runEphemeralPhase` which spawns
   `claude --append-system-prompt <plan prompt> ...` via Docker exec.
4. The fake claude reads the system prompt, sees `PLAN_DRAFTED` instructed,
   emits `PLAN_DRAFTED` + fenced JSON payload, exits 0.
5. Runner parses the marker, transitions to CreatingSubtasks, and
   onward through execute/document/review/commit. Each phase's
   `claude` exec hits this stub.

## Phase detection

The stub detects which phase it's faking by substring-matching the
system prompt for marker names:

| Prompt contains | Stub emits |
|---|---|
| `PLAN_DRAFTED` | PLAN_DRAFTED + JSON payload (PlanDraftedPayload) |
| `TASK_COMPLETE` or `TASK_NEEDS_DECOMPOSITION` | TASK_COMPLETE |
| `DOCS_WRITTEN` | DOCS_WRITTEN |
| `REVIEW_FINDINGS` | REVIEW_FINDINGS + JSON (recommendation: approve) |
| `DIAGNOSIS_COMPLETE` | DIAGNOSIS_COMPLETE + JSON |

## Build

```
docker build -t cm-stub-orchestrated:test test/integration/stub-worker/
```

The harness builds this automatically on first run.
