# Architecture

## Trust model

ContextMatrix has two authentication postures, controlled by `auth.mode` in
config (`AuthConfig` in `internal/config/config.go`; env
`CONTEXTMATRIX_AUTH_MODE`). **`multi` is the default** when the field is left
unset (`applyAuthDefaults`); **`none`** is CM's single-tenant,
zero-login behavior. The router-level switch is a nil vs. non-nil
`*auth.Service`: `RouterConfig.AuthService == nil` skips every auth route, the
session-guard middleware, and the admin routes entirely, leaving the router
byte-for-byte identical to single-user CM (`internal/api/router.go`
documents this as "the auth.mode 'none' guarantee"). The two modes have
materially different security properties — the material below is organized by
mode, then by what stays constant across both.

**`auth.mode: none` is single-tenant and unauthenticated by design.** There
are no user accounts, no logins, no per-user permissions, no session tokens.
Deployment assumes loopback or a trusted-network ACL (firewall,
NetworkPolicy, service-mesh rule) — same posture as the admin/debug listener
documented in `docs/api-reference.md`.

The implications for code review, in none mode:

**Identity is not authentication.** The `X-Agent-ID` header tags writes for
audit purposes — boards-repo commit author, activity-log entries,
`assigned_agent` on cards. It is treated like `git config user.name` on a
personal machine: useful for blame, trivial to spoof, and that's fine because
there is no permission gradient to escalate into.

**The web UI auto-generates a per-browser identity.**
`web/src/hooks/useAgentId.ts` mints `human:web-<8 hex chars>` on first visit,
persists it in localStorage, and wires it into every API request via
`api.setAgentId`. `useIdentity` (`web/src/hooks/useIdentity.ts`) only invokes
this hook when the auth mode is `none`; in multi mode it derives identity from
the logged-in session instead (below). We do **not** prompt users for
usernames in none mode — there is nothing to authenticate them against, so a
prompt is theatre. Per-browser uniqueness prevents two tabs/users from
accidentally releasing each other's card claims; that is the only reason a
unique-per-browser id is needed.

**REST writes that require an identity but receive none fall back to a marker
identity.** `internal/api/runner.go` falls back to `human:api` for the
human-only runner endpoints; `agentIDForChat` in `internal/api/chats.go` falls
back to `human:web`. These markers are honest ("this came from the web UI /
direct API call by an unspecified human") and they preserve write
functionality without inventing fake usernames. In multi mode both fallbacks
are unreachable dead code on the routes that use them — the session guard has
already rejected any request with no session before the handler runs (below).

**Where identity gates do exist, they enforce workflow contracts, not access
control (in none mode):**

- **Card claim / heartbeat / release**: the supplied `X-Agent-ID` must match
  `assigned_agent`. This stops two agents from accidentally clobbering each
  other's claim — it does not stop a malicious caller (who can simply send the
  matching value). In multi mode the identity comes from the session instead
  of a spoofable header, so this same check becomes real ownership enforcement
  (below).
- **MCP human-only tools** (`promote_to_autonomous`): the `agent_id` argument
  must start with `human:`. The check rejects callers that follow the agent
  convention of using a non-human identifier (e.g. `agent-foo`); a malicious
  caller can pass `human:anything` and the gate yields. The intent is to encode
  "this operation is part of the human workflow," not to prevent forgery —
  true in multi mode too, since MCP never gained a session concept.
- **Human-only operations on cards** (e.g. flipping `autonomous: true` via
  `PromoteToAutonomous` / the `promote_to_autonomous` MCP tool): the same
  `human:` prefix check, same intent. The REST handler in
  `internal/api/runner.go` falls back to `human:api` when `X-Agent-ID` is absent
  so the service-layer gate still passes for direct API calls in none mode (in
  multi mode the session identity already satisfies it).

**`auth.mode: multi` adds real authentication: login, sessions, and an admin
role.** Users, sessions, one-time tokens, and the GitHub credential pool are
persisted in `auth.db` (`internal/authstore`). On first start with zero users,
`main.go` issues a one-time bootstrap token and logs the redemption URL;
opening it creates the first admin account.

**The session guard gates almost the entire API, not just writes.**
`sessionGuard` (`internal/api/auth.go`) runs on every request in multi mode
and rejects any request with no valid session unless the path is exempt
(`sessionExempt`): `/healthz`, `/readyz`, `/mcp`, `/api/auth/*`,
`/api/app/config` (serves a slim payload pre-login), and the HMAC-signed
backend-callback prefixes (`/api/runner/*`, `/api/agent/*`, `/api/chat/*`,
`/api/v1/*` — with `/api/runner/logs` and `/api/runner/health` explicitly
carved back out of the exemption because they are browser-facing despite the
prefix). Board reads (`GET /api/projects`, `GET /api/events`, and so on)
require a session exactly like writes do; multi mode makes no read/write
distinction.

**Sessions.** Passwords are hashed with argon2id, with parameters embedded in
the stored hash so they can be strengthened later without a migration
(`internal/auth/password.go`). A session is a random 256-bit token; only its
SHA-256 hash is ever persisted (`internal/auth/token.go`), so a stolen
`auth.db` yields no usable session. The cookie (`cm_session`) is `HttpOnly` +
`SameSite=Lax` always, and `Secure` whenever the request arrived over TLS
directly or via `X-Forwarded-Proto` (`requestIsTLS`). Sessions idle past a
5-minute threshold get a sliding renewal to `now + session_idle_ttl` (default
720h / 30 days) on the next validated request. Because cookies and the
bootstrap/invite/reset links carry bearer-equivalent secrets, multi mode
expects TLS termination in front (reverse proxy or ingress) so they never
cross an untrusted network in the clear — CM does not enforce this at the
application layer.

**Identity is derived from the session and is authoritative.**
`withSessionIdentity` stamps `human:<username>` into the request context;
`extractAgentID` (`internal/api/agents.go`) checks the context first and only
falls back to the `X-Agent-ID` header when there is no session — so a
logged-in browser cannot claim a different identity by header. This is what
upgrades the card-claim/heartbeat/release ownership check (above) from a
courtesy into real enforcement in multi mode: another authenticated user
genuinely cannot forge your claim identity.

**The admin role is a narrower gate layered on top of login, not a
replacement for it.** A user record carries `is_admin`. `requireAdmin`
(`internal/api/admin.go`) returns 403 `FORBIDDEN` for a logged-in non-admin and
gates: user management (`GET`/`POST /api/admin/users`,
`PATCH /api/admin/users/{username}`, invite regeneration), the GitHub
credential pool (`GET`/`POST /api/admin/credentials`,
`PUT`/`DELETE /api/admin/credentials/{name}`), and project management
(`POST`/`PUT`/`DELETE /api/projects*`,
`POST /api/projects/{project}/recalculate-costs` — see the `authEnabled` /
`requireAdmin` calls in `internal/api/projects.go`). Ordinary card work —
claim, release, update, transition, activity — needs only a valid session, any
role. The store refuses to demote or disable the last active admin
(`ErrLastAdmin`), enforced as a guarded atomic update rather than a
check-then-write race.

**The GitHub credential pool holds encrypted secrets; project bindings scope
GitHub operations.** Admins register named credentials (GitHub App or PAT) via
`/api/admin/credentials`; each secret is AES-256-GCM-encrypted
(`internal/auth/crypto.go`) under a key HKDF-derived from the auth master key
(`internal/auth/masterkey.go`; `master_key_file` is auto-generated 0600 on
first start, with a log warning to move it into real secret management). A
project's `.board.yaml` `github_credential` field binds it to one pool entry;
`TokenProviderFor` (`internal/auth/credentials.go`) resolves that binding into
a cached `githubauth.TokenGenerator`, scoping that project's GitHub operations
(boards push, issue import, branch listing) to the bound credential.
`newProviderForProject` (`cmd/contextmatrix/provider.go`) builds this
resolver — `main.go` only wires it (as `providerForProject`) into
`RouterConfig` and the GitHub-issue-sync path — and it fails closed on a
broken binding, never silently substituting the instance-wide credential.
Projects with no binding keep using the instance-wide `githubauth` provider,
identical to none-mode behavior.

**Operator escape hatches run on the host, outside the HTTP surface
entirely.** `contextmatrix auth reset-admin <username>` and
`contextmatrix auth rotate-master-key` (`cmd/contextmatrix/authcli.go`) are
multi-mode-only CLI subcommands: each loads config the same way the server
does (`--config` flag, else XDG discovery) and then opens `auth.db` directly.
Host access to run this binary against the server's config is root trust,
same posture as any other operator escape hatch. `reset-admin <username>`
prints a one-time, 48-hour password-reset link for an existing, enabled admin
— the recovery path when every admin account is locked out. `rotate-master-key`
re-encrypts the entire credential pool under a freshly generated master key
inside one `auth.db` transaction: the new key is staged at `<path>.new` before
the transaction commits, installed over `<path>` once it has committed, and
the previous key is saved to `<path>.bak` for reference only — restoring it
does NOT roll the rotation back, since the pool is already re-encrypted under
the new key by the time the file swap happens. Run both on the host against
the same config file the running server uses, and restart the server
afterward so it loads the new key file. Safest is to stop the server first;
at minimum, do not create or rotate pool credentials through the *live*
server between `rotate-master-key`'s commit and that restart —
`SetCredentialKey` (`cmd/contextmatrix/main.go`) wires the HKDF-derived
credential key into `auth.Service` once, at startup, so a running process
keeps encrypting under the OLD key regardless of what the CLI does to the key
file. `CreateCredential` and `RotateCredentialSecret`
(`internal/auth/credentials.go`) only ever encrypt — they never decrypt
existing pool data — so such a write still succeeds in the moment; it just
produces a pool entry that fails to decrypt once the server is restarted and
starts reading the rest of the (now-rotated) pool under the new key.

**What stays constant across both modes — the machine channels:**

- **MCP Bearer token** (`mcp_api_key` config) gates `/mcp`, independent of
  `auth.mode`. Optional for loopback deployments; set it whenever `/mcp` is
  reachable over a network, in either mode.
- **Runner webhook HMAC** (shared secret in config) authenticates
  `contextmatrix-runner` / `contextmatrix-agent` callbacks into the server,
  independent of `auth.mode`. The backend is on a different host; the secret
  prevents arbitrary network callers from injecting status updates.
- **`/healthz` and `/readyz`** are open in both modes (`sessionExempt` lists
  them explicitly; there is no session guard at all in none mode).
- **The admin listener** (pprof + `/metrics`, `admin_port`) is loopback-only
  in both modes — see `docs/api-reference.md`.
- **The CSRF gate** (`csrfGuard` in `internal/api/router.go`, requiring
  `X-Requested-With: contextmatrix`) is unconditional middleware in both
  modes, independent of session auth — it defends against cross-origin
  browser requests, a separate concern from the session guard's defense
  against unauthenticated ones.
- **GitHub authentication** via the shared
  `github.com/mhersson/contextmatrix-githubauth` module (App or PAT) is real
  auth against an external system in every mode; do not weaken or bypass.

**MCP's project-management tools are NOT behind the admin gate, in either
mode — this is not a gap.** `create_project`, `update_project`, and
`delete_project` (`internal/mcp/tools_projects.go`) call `CardService`
directly with no role check; MCP has no admin/role concept at all, for any
tool — the Bearer-key check on `/mcp` is the only gate, uniform across every
tool. This is safe because `updateProjectToolInput` (same file) has no
`github_credential` field, so the MCP `update_project` tool cannot touch
credential bindings by construction — there is no privilege-escalation path
from "holds the MCP bearer key" to "controls the credential pool." Contrast
the REST `PUT /api/projects/{project}` handler, which does accept
`github_credential` and is admin-gated in multi mode
(`internal/api/projects.go`).

**"UI = human" holds in both modes, with different strength.** In `none` mode
the web UI is operated by an unauthenticated human behind the CSRF gate — a
convention, not a proof. In `multi` mode the UI is operated by an
authenticated, session-bound human — a proof.

**What to do during a code review:**

- In `none` mode: treat any "missing X-Agent-ID is a security hole" or
  "fabricated human:web is identity spoofing" finding as **out of scope** —
  it's the documented trust model there. If the deployment posture is wrong
  (CM exposed publicly without a network gate), that's an ops concern, not a
  code-fix concern.
- In `multi` mode: a state-changing or read request reaching a handler without
  a valid session is a real bug — `sessionGuard` should have rejected it
  upstream. A new user-management, credential-pool, or project-management
  route that skips `requireAdmin` is a real finding; a new card-scoped route
  (claim, update, transition, chat) that adds `requireAdmin` is over-gating in
  the other direction.
- Do not propose admin-gating the MCP project tools "to match REST" — see
  above. MCP has no role concept; fixing that would mean redesigning MCP auth
  entirely, not a one-line change.
- The MCP human-only checks are workflow gates in every mode; do not propose
  tightening them to session-backed auth without changing the trust model
  first.
- The browser-generated agent ID (none mode) is intentional. Do not propose
  adding a username prompt, OAuth, or per-user permissions to none mode —
  that's what multi mode is for.
- `githubauth` is the one place where real authentication matters in every
  mode. Token-handling code there should be reviewed strictly.

## Data flow

Every HTTP request walks the middleware chain defined in
`internal/api/router.go`:

```text
recovery → securityHeaders → [cors] → requestID → observe → bodyLimit → csrfGuard → mux
```

`recovery` catches panics, `securityHeaders` sets the static security headers
and CSP, `cors` (only registered when `cors_origin` is non-empty) emits the CORS
preamble, `requestID` mints or accepts an `X-Request-ID` and stashes a
request-scoped `*slog.Logger` in context via `ctxlog.WithRequestID`, `observe`
records RED metrics + emits the per-request log line, `bodyLimit` caps inbound
bodies at 5 MB (with per-route overrides via `bodyLimitOverrides` — currently
`POST /api/images` gets the 11 MB image-upload envelope so screenshots fit),
and `csrfGuard` rejects state-changing requests that lack
`X-Requested-With: contextmatrix` (with narrow exemptions: GET/HEAD/OPTIONS,
`/healthz`, `/readyz`, `/api/runner/*`, `/api/agent/*`, `/api/chat/*`, and
`/mcp`).

Card mutations follow the same pipeline through the service layer:

```text
API handler (deserialize, validate)
  → CardService.<Mutation>
    → writeMu.Lock()
    → Validator.ValidateCard()    — type, state, priority checks
    → Store.UpdateCard()/CreateCard()
                                  — write .md file under storage's writeMu,
                                    update in-memory index
    → enqueueCardCommit(...)      — push gitops.CommitJob on the per-project
                                    queue (or run inline when no queue is wired)
    → writeMu.Unlock()
    → awaitCommit(...)            — block on the queue result without holding
                                    writeMu, so other writers don't stall
    → events.Bus.Publish()        — notify SSE subscribers
  ← return card
← serialize response
```

The MCP server follows the same path — it calls `CardService` methods, never the
store or git layer directly. The `/mcp` handler is registered on the same inner
`http.ServeMux` as the REST API, so MCP traffic shares every middleware listed
above plus an inner stack
(`mcpAuthMiddleware → clearWriteDeadlineForStreaming → chatSessionHeaderMiddleware → mcpRequestInfoMiddleware → SDK handler`).

## Async-commit consistency

Card mutations take an eager-write, async-commit shape:

1. `store.Update*` writes the new card state to the in-memory cache and to disk
   under `writeMu`.
2. The git commit is enqueued via `gitops.CommitQueue.Enqueue` (when a queue is
   wired; otherwise executed inline) and awaited **after** `writeMu` is released
   so slow go-git operations do not block concurrent writers.

`gitops.CommitQueue` runs one worker goroutine per project; commits for the same
project execute strictly in enqueue order, but different projects commit in
parallel. Workers are spawned lazily on first enqueue, and (when constructed
with `WithIdleTimeout`) tear themselves down after a configurable idle window —
`main.go` wires the production queue with a 30-minute idle timeout so long-quiet
projects free their goroutine. The queue exposes `Pause` / `AwaitIdle` so the
gitsync layer can drain in-flight commits before running a shell rebase or push;
`CardService.LockWrites` calls these in sequence.

This means cache + disk can be ahead of git for the window between store write
and commit completion. The service layer closes that gap on failure:

- **Commit success (typical path):** all three substrates (cache, disk, git)
  converge and the caller sees the new card.
- **Commit failure:** `applyCardMutation`, `DeleteCard`, `AddLogEntry`,
  `ClaimCard`, `ReleaseCard`, `markCardStalled`, `RecordPush`,
  `IncrementReviewAttempts`, `UpdateRunnerStatus`, `PromoteToAutonomous`, and
  `ReportUsage` snapshot the pre-mutation card via `store.GetCard` (which
  returns a deep copy) before mutating, then reapply that snapshot via
  `store.UpdateCard` (or `store.CreateCard` for `DeleteCard`) after a failed
  commit. The caller receives `fmt.Errorf("git commit: %w", err)` — equivalent
  to the pre-async behaviour.
- **Rollback failure (rare):** cache and disk become inconsistent with each
  other. A `slog.Error` line carrying `committed=false`, `rollback_failed=true`,
  the card ID, and both errors is emitted for operators; the returned error is
  the `errors.Join` of the original commit error (wrapped with "rollback failed,
  state inconsistent") and the rollback error. The
  `contextmatrix_rollback_failures_total` counter increments on every such
  event. **Alerting:** page on any non-zero rate — each increment is a
  data-integrity event that leaves the named card's cache + on-disk state
  diverged and requires manual reconciliation (typically: inspect the error log
  for the card ID, then re-run the mutation or restore from the git HEAD copy).
- **Heartbeats are a deliberate exception:** `HeartbeatCard` does not roll back.
  A failed heartbeat commit is self-healing — the next heartbeat (typically
  within the heartbeat interval) produces another commit and restores
  consistency.
- **Parent auto-transitions are a deliberate exception:** they are
  fire-and-forget from the child write path (`maybeTransitionParent` →
  `transitionParentDirect`). A failed commit increments
  `contextmatrix_parent_autotransition_errors_total` and logs a Warn; the next
  parent mutation re-commits the state.

## Component responsibilities

- **Store** (`storage.FilesystemStore`): reads/writes `.md` files and
  `.board.yaml` to disk. Maintains an in-memory index. No knowledge of git,
  events, or locking.
- **gitops.Manager** (`gitops.Manager`): stages and commits files, handles
  push/pull with remote repositories. No knowledge of cards or events.
- **Lock Manager** (`lock.Manager`): enforces claim/release/heartbeat rules.
  Reads cards via the store to check ownership but does not write — it returns
  modified card data to the caller (the service layer).
- **Event Bus** (`events.Bus`): in-process pub/sub. Receives events, fans out to
  subscribers.
- **Validator** (`board.Validator`): validates transitions and card fields. Pure
  functions, no side effects.
- **CardService** (`service.CardService`): the only component that orchestrates
  multi-step operations. Every mutation follows: validate → store write → git
  commit → event publish. Also runs the heartbeat timeout checker goroutine,
  which lives here (not in the lock manager) because it coordinates store, git,
  and events. Satisfies the `chat.Pricer` interface (via `CardService.PriceTokens`,
  wired into `chat.Config.Pricer`) so chat-session cost frames share the same
  cache-tier formula as card-scoped `report_usage`. Holds a `chatCostSummarizer`
  field (wired via `SetChatCostSummarizer`) that, when non-nil, is called on each
  `GetDashboard` invocation to append server-wide chat-cost aggregates to the
  per-project `DashboardData` payload.
- **Session Log Manager** (`runner/sessionlog.Manager`): server-side per-card
  SSE buffer and fan-out hub. Keeps a single long-lived authenticated upstream
  connection to the runner per active card, tees events into a bounded ring
  buffer, and replays the buffer snapshot to every new subscriber before tailing
  live events. Started by `CardService.UpdateRunnerStatus` on `→running`,
  stopped (fire-and-forget) on terminal statuses. See `docs/remote-execution.md`
  § Session Log Manager for full details.
- **chat.Manager** (`chat.Manager`): orchestrates the global chat surface
  (project-agnostic chat sessions that share the runner's worker image but use
  long-lived containers instead of card-scoped one-shots). Owns session
  lifecycle (`cold` → `active` → `warm-idle` → `ending`), persists the
  transcript through `chat.Store`, delegates container management to a
  `chat.Backend` (HMAC-signed calls to the runner's `/chat/start` and
  `/chat/end`; the sole implementation is `NewRunnerClient`), and bridges the
  runner's `/logs?session_id=` SSE feed back into
  the transcript by appending each entry through `AppendMessage`. Holds `m.mu`
  across the seq-assignment + store insert so disk insertion order matches seq
  order regardless of writer concurrency. On cold-reopen,
  `chat.transcript.Build` produces the resume payload shipped to the runner;
  while `RehydrationActive` is true on the session row, `AppendMessage` stamps
  incoming entries with `rehydration_phase=TRUE` so the next reopen can filter
  them out. The MCP tool `chat_rehydration_complete` flips the flag back to
  false and persists the agent's summary as the first visible message. The
  `chat_rehydration_complete` MCP tool is gated by the calling container's
  `CM_CHAT_SESSION` (forwarded as `X-CM-Chat-Session`): a caller can only flip
  its own session's rehydration flag.

  **Cost accumulation:** each `usage` stream-json frame from the runner log
  stream reaches `handleUsageEntry`. Usage frames carry per-turn
  (per-assistant-message) token counts — the runner emits one
  `message.usage` block per assistant turn following the Anthropic
  Messages-API contract; these are NOT cumulative session totals. The
  values are passed directly (no snapshot subtraction) to
  `chat.Pricer.PriceTokens(model, prompt, cacheRead, cacheCreate,
  completion)`. The `chat.Pricer` interface (defined in
  `internal/chat/pricer.go`; satisfied by `*service.CardService`) applies
  the same cache-tier cost formula used on the card-scoped `report_usage`
  path. `Store.IncrementSessionCost` persists the
  result via a single atomic `UPDATE ... SET col = col + ? ... RETURNING ...`
  — one SQL round-trip, no read-modify-write window. On persist error the
  function returns without publishing an SSE `session_updated` event, mirroring
  the `UpdateContextTokens` early-return pattern. On success, a
  `session_updated` event carrying the new running totals (`prompt_tokens`,
  `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`,
  `estimated_cost_usd`) is published to the per-session SSE hub so the chat
  header cost indicator updates in real time. `GetChatCostSummary` aggregates
  server-wide chat cost over a 30-day UTC window; the result is cached for 30
  seconds (`chatCostCacheTTL`) to prevent N× SQL amplification when the All
  Projects view fans out one dashboard request per project. See
  [`docs/api-reference.md`](api-reference.md)
  § `GET /api/projects/{project}/dashboard` for the full field specification
  and caching semantics.

- **chat.Transcript** (`chat/transcript`): pure transcript-shaping function — no
  I/O, no state. `Build(messages, opts)` filters out `rehydration_phase=TRUE`
  entries, drops non-conversation roles (stderr, tool results), pins the first
  user turn and the last 20 turns, and truncates middle turns to fit
  `chat.resume_budget_tokens` (default 40k). Returns the kept rows plus a `Meta`
  describing whether the budget clipped older content. Called only on
  `/api/chats/{id}/open` when the session has prior messages.
- **chat.Store** (`chat.Store`, default impl `opstore/sqlite.Store`):
  SQLite-backed persistence for `chat_sessions`, `chat_messages`, and
  `chat_cost_archive`. Schema created by `ensureSchema` in
  `internal/opstore/sqlite/schema.go` — a clean-cut `CREATE TABLE IF NOT EXISTS`
  create with no migration ledger (see `docs/data-model.md` for column details).
  The store lives in the shared `ops.db`, which also holds the model blacklist.
  WAL mode with `MaxOpenConns=5`
  so concurrent readers (`ListMessages`, `MaxSeq`, `GetSession`) bypass the
  single-writer gate that `chat.Manager.mu` enforces above the pool. Unique index
  on `(session_id, seq)` is the safety net behind the in-memory seq cache.
  `DeleteSession` archives cost columns to `chat_cost_archive` before hard-deleting
  the session row, so `AggregateCost` can `UNION ALL` both tables and preserve
  deleted sessions' spend in the dashboard summary.
- **chat.SSEHub** (`chat.SSEHub`): per-session SSE fan-out. Each `sessionHub`
  has a 128-entry ring buffer of recent events and a subscriber set; replays the
  ring on `Subscribe(sinceSeq)` so reconnects within the ring window are
  gapless. `Manager.DeleteSession` calls `Hub.Drop(sessionID)` to release the
  per-session hub so memory does not grow with session churn. Two event kinds
  share the hub: `message` (a new transcript row, with seq + role + content) and
  `session_updated` (a metadata change — `context_tokens`, `rehydration_active`,
  model, `status` for lifecycle transitions, and the five cost/token running-total
  fields (`prompt_tokens`, `completion_tokens`, `cache_read_tokens`,
  `cache_creation_tokens`, `estimated_cost_usd`) — with no transcript content).
  The `status` field uses a pointer so `omitempty` distinguishes "no lifecycle
  change" from a deliberate transition.

  Server-side, `publishSessionUpdate` fans out the event in a goroutine so callers
  holding a sessionHub lock don't deadlock. Lifecycle entry points that emit `status`:
  - `OpenSession` — cold→active and warm-idle→active
  - `OnSubscribe` callback — warm-idle→active
  - `MarkWarmIdle` — active→warm-idle
  - `EndSession` — any→cold (paired with `RehydrationActive: false` when the
    persist succeeded)

  Client-side, `useChatStream` routes `session_updated` events into header state.
  When the `status` field changes, a `prevStatusRef` ref (scoped to the SSE handler)
  detects the transition exactly once per real event and calls
  `notifyChatSessionsChanged()` directly — `useChatSessions` debounces that event
  with a 100 ms window to coalesce fan-out from multiple open panes into a single
  `/api/chats` refetch that updates the sidebar status dot.
- **chat.IdleReaper** (`chat.IdleReaper`): scans `warm-idle` sessions older than
  `IdleTTL` and ends them. `Stop()` is `sync.Once`-guarded so repeated shutdown
  calls don't panic.
- **images.Store** (`internal/images`): content-hashed image blob store backing
  the paste / drag-drop screenshot upload flow. SQLite-backed (`images.db`,
  separate from the board and chat DBs). IDs are
  `sha256(processed_bytes)[:16]` so identical uploads dedup naturally and URLs
  are stable. The processor enforces a 10 MB cap, resizes to fit 1024x768
  preserving aspect ratio (CatmullRom from `golang.org/x/image/draw`),
  re-encodes in the same format (strips EXIF naturally), and rejects animated
  GIFs / non-image MIME types. Wired into `api.NewRouter` for `POST /api/images`
  + `GET /api/images/{id}` and into `mcp.NewServer` for the inline image
  attachments on `get_card` / `get_task_context`.
- **auth.Service** (`internal/auth`): multi-mode authentication. Argon2id
  password hashing + session issuance/validation (`password.go`, `token.go`),
  one-time bootstrap/invite/reset tokens (`tokens.go`; 48h TTL), and the GitHub
  credential-pool crypto (`crypto.go`, `masterkey.go`) — AES-256-GCM secrets
  under a key HKDF-derived from the auth master key. `TokenProviderFor`
  resolves a project's `.board.yaml` `github_credential` binding into a cached
  `githubauth.TokenGenerator`, fail-closed on a broken binding (see Trust
  model above). Nil (`RouterConfig.AuthService == nil`) in `auth.mode: none`.
- **authstore.Store** (`internal/authstore`): SQLite persistence backing
  `auth.Service` — `auth.db` holds the `users`, `sessions`, `one_time_tokens`,
  and `credentials` tables. No business logic; `auth.Service` is its only
  caller.
- **API handlers** (`api/*`): thin HTTP layer. Deserialize → call CardService →
  serialize. No business logic, no direct store/git/lock access.
  `GET /api/runner/logs` has two modes: card-scoped (subscribes to one card's
  session) and project-scoped (subscribes to the project session, fanning out
  every card's events). Both replay the buffered snapshot through the session
  manager before tailing live events.
- **MCP server** (`mcp/*`): exposes tools (card operations) and prompts (skill
  files) via Streamable HTTP on `/mcp` (registered for `POST`, `GET`, and
  `DELETE`). Registered on the same `http.ServeMux` as the REST API, so it
  inherits the shared middleware chain (recovery, security headers, CORS,
  requestID, observe, bodyLimit, csrfGuard) with no special wrapping. The
  body-limit defaults to 5 MB; `bodyLimitOverrides` in `internal/api/router.go`
  raises the cap for file-bearing routes (`POST /api/images` gets 11 MB).
- **Context-aware logger** (`ctxlog`): stores a `*slog.Logger` enriched with a
  `request_id` attribute in the request context. The `requestID` middleware in
  `internal/api/` calls `ctxlog.WithRequestID(ctx, id)` on every incoming
  request. All log sites in `internal/api/`, `internal/service/`,
  `internal/storage/`, and `internal/runner/` retrieve the logger via
  `ctxlog.Logger(ctx)` so every log line emitted during a request carries the
  same correlation ID. Falls back to `slog.Default()` for background contexts
  that bypass the middleware (e.g. stall scanner goroutine). Also stores a
  `*MCPCall` in the context (via `ctxlog.WithMCPCall`) for `/mcp` requests;
  `mcpRequestInfoMiddleware` in `internal/mcp/server.go` populates it with the
  JSON-RPC `method` and tool `name`, which the `observe` middleware then appends
  as `mcp_method` / `mcp_tool` fields on the per-request log line.
- **Clock** (`clock`): tiny `clock.Clock` interface with `Real()` and a fake
  implementation used by tests. `lock.Manager`, `CardService`, and `chat.Manager`
  all read time through this interface so a single fake drives every
  time-sensitive subsystem deterministically. The service layer adopts the lock
  manager's clock so stall detection and the timeout-checker ticker share one
  monotonic reading — wiring two different clocks across these subsystems is a
  latent test-flake source.
- **Event Bus** (`events.Bus`): in-process publish/subscribe. The bus has a drop
  counter (`contextmatrix_event_bus_drops_total`) — subscribers that fall behind
  the per-subscriber channel cap drop events rather than blocking the publisher.
- **gitsync Syncer** (`gitsync.Syncer`): background loop that pulls the boards
  remote (when `boards.auto_pull` is enabled) and pushes after each successful
  commit (when `boards.auto_push` is enabled). Coordinates with the service
  layer through `LockWrites`/`UnlockWrites` and with the commit queue through
  `Pause`/`Resume`/`AwaitIdle` so rebases never race against in-flight go-git
  commits.
- **GitHub integration** (`github`): three pieces — `client.go` (HTTP client for
  GitHub REST API used during issue import / branch listing), `parse.go` (issue
  → card mapping rules), `syncer.go` (per-project import loop driven by
  `github.import_issues`). Auth is delegated to the shared
  `githubauth.TokenGenerator` provider; the package never reads tokens directly.
- **Config** (`config`): typed YAML loader. Every field has a documented
  `CONTEXTMATRIX_*` env override; `config.yaml.example` is the canonical
  reference.
- **Metrics** (`metrics`): declares all Prometheus metric vars and exposes a
  `Register(prometheus.Registerer)` function called once at startup in
  `main.go`. Metrics are served at `GET /metrics` on the **admin listener** only
  (`admin_port`, bound to `admin_bind_addr`; default loopback). The main
  listener does not expose `/metrics`. The `observe` middleware in
  `internal/api/` wraps every REST route to record per-route HTTP RED
  (rate/error/duration) metrics; unmatched routes collapse to a single
  `path="unmatched"` label to bound cardinality. SSE endpoints are excluded from
  the latency histogram because their connection lifetime would drown out real
  REST signal. Additional instrumentation: SSE gauge in `internal/api/events.go`
  and `runner_logs.go`, event-bus drop counter in `internal/events/`, git-sync
  histogram in `internal/gitops/`, stall-scanner histogram and counter in
  `internal/service/`, unknown-model counter
  (`contextmatrix_report_usage_unknown_model_total`, labeled by model) in
  `internal/service/service_usage.go` (incremented when `report_usage` is called
  with a model absent from `token_costs` — alert on a sustained non-zero rate to
  detect misconfigured or newly deployed models). Two chat-specific counters are
  also registered: `contextmatrix_chat_usage_unknown_model_total` (labeled by
  model, incremented in `handleUsageEntry` when a chat usage frame references an
  unpriced model) and `contextmatrix_chat_cost_summary_errors_total` (unlabeled,
  incremented when `GetChatCostSummary` fails during a `GetDashboard` call). See
  the full metric list in `internal/metrics/metrics.go`.

## Git repository scope

The boards directory is a separate git repository from the source code. The
`gitops.Manager` operates on `cfg.Boards.Dir`, not the source tree. File paths
passed to `CommitFile()` / `CommitFiles()` are relative to that directory (e.g.,
`project-alpha/tasks/ALPHA-001.md`).

```text
~/code/contextmatrix/           # source code repo
  cmd/, internal/, web/, workflow-skills/
  config.yaml                   # boards.dir: ~/boards/contextmatrix

~/boards/contextmatrix/         # boards repo (separate git repo)
  project-alpha/
    .board.yaml
    tasks/
    templates/
```

If the boards directory does not exist or is not a git repo on startup, the
server creates it and runs `git init`.

`boards.dir` in `config.yaml` should point outside the source tree — an absolute
path or a path like `~/boards/contextmatrix`, not `./boards`.

## File layout

**Source code:**

```text
cmd/contextmatrix/main.go
internal/
  board/             # domain types + Validator + state machine
  storage/           # FilesystemStore + Store interface
  gitops/            # gitops.Manager + CommitQueue (per-project workers)
  lock/              # claim/release/heartbeat + stall scan
  service/           # CardService orchestration (split across service_*.go)
  api/               # REST handlers + SSE + middleware chain + CSRF gate
  mcp/               # MCP server (Streamable HTTP /mcp) + mcpcontext/
  runner/            # webhook client + replay cache + reconciler (HMAC via contextmatrix-protocol)
    sessionlog/      # per-card SSE buffer + fan-out hub
  chat/              # chat.Manager + Store + SSEHub + IdleReaper + runner bridge
    transcript/      # pure transcript-shaping for cold-reopen resume payloads
  opstore/           # shared operational SQLite store (chat + model blacklist)
    sqlite/          # ensureSchema + Store impl (ops.db)
  images/            # content-hashed image blob store + processor (resize/EXIF strip)
  github/            # GitHub client + issue parser + import syncer
  gitsync/           # boards repo background pull/push syncer
  events/            # in-process pub/sub (events.Bus)
  config/            # typed YAML loader
  ctxlog/            # request_id context logger + MCPCall context
  metrics/           # Prometheus metric vars + Register()
  clock/             # injectable clock (Real + fakes for tests)
web/                 # React + Vite frontend (embedded via web/embed.go)
workflow-skills/     # skill markdown files served via MCP prompts
go.mod
config.yaml.example
Makefile
```

**Boards repo:**

```text
project-alpha/
  .board.yaml
  templates/
    task.md
    bug.md
    feature.md
  tasks/
    ALPHA-001.md
    ALPHA-002.md
project-beta/
  .board.yaml
  templates/
  tasks/
```
