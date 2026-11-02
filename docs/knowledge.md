# Knowledge providers

A knowledge provider is an MCP server that holds what a product is meant to
do: its goals, agreed rules, open decisions, architecture, and where today's
system differs from the rules. A project names one provider and a scope, and
ContextMatrix (CM) serves three read-only MCP tools that answer from it, so
any MCP client can read the product's knowledge next to the board.

CM is a client and a pass-through. It reads knowledge and never writes it back
or summarises it; it keeps answers only in a short in-memory item cache and in
its audit log, and it knows nothing about how a provider keeps them. A
provider can be a service with search and ranking, or a plain folder of
markdown behind a small MCP server with no AI at all. This page is complete on
its own for a provider author: the [provider contract](#the-provider-contract)
is what to implement.

Without `knowledge.providers` in the config nothing changes: the knowledge
tools are not registered and no audit log is written.

## Configuring providers

```yaml
knowledge:
  log_path: ""        # default <state>/knowledge.log; env CONTEXTMATRIX_KNOWLEDGE_LOG_PATH
  providers:
    - name: intent
      url: https://knowledge.example/mcp
      token: bearer-secret
```

| Field                        | Rule                                                                                                                                                                       |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `providers[].name`           | Required, unique, matches `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Projects refer to it, the Project Settings dropdown shows it, every audit line carries it, and so does a result tagged `source: "knowledge:<name>"` (see [`source`](#cms-knowledge-tools)). |
| `providers[].url`            | Required. The provider's MCP endpoint (Streamable HTTP). `https://`, or `http://` only when the host is loopback: `localhost`, `127.0.0.0/8` or `::1`. No credentials in the URL. |
| `providers[].token`          | Required. Sent as `Authorization: Bearer <token>`.                                                                                                                         |
| `log_path`                   | The [audit log](#the-audit-log). Env `CONTEXTMATRIX_KNOWLEDGE_LOG_PATH`; tilde expansion applies.                                                                           |

`providers` is config-file only: the token is a secret and env overrides are
scalar-only, so there is no `CONTEXTMATRIX_*` override for it (the same as
`mob.guests`). Changing a provider means editing the config and restarting.

Startup validates every entry: an entry that breaks a rule above stops startup
with an error naming `knowledge.providers[<index>]`. An unreachable provider
is not a startup error: CM connects on first use, and a provider that does not
answer is absent until it does (see [availability](#availability)).

## Project fields

Two `.board.yaml` fields point a project at a provider:

```yaml
knowledge_provider: intent   # a knowledge.providers name
knowledge_scope: payments    # the product's name at the provider
```

The scope is a name only the provider understands; several projects may share
one. A human sets both in Project Settings, Source tab: a dropdown of the
configured provider names (served as `knowledge_providers` by
`GET /api/app/config`, names only) and a scope text field. With no providers
configured and none set, the section says so and offers no choice. Saving
through `PUT /api/projects/{project}` rejects a provider the server does not
configure and requires a scope of at most 200 characters when a provider is
set. MCP `update_project` preserves both fields and cannot set them.

A project that names a provider the server does not configure (a hand edit, or
a provider removed from the config) still loads. While the server configures
other providers, its knowledge tools answer empty with a `reason`, and CM logs
a warning on each call when the project also has a scope; with no providers
configured the tools are not registered at all.

## CM's knowledge tools

| CM tool                 | Arguments                                                                                                              | Result (structured content and its JSON text)            |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| `get_knowledge_context` | `project` or `card_id` (one required), `task` (string, required), `limit` (int, optional), `caller` (string, optional) | `{"source", "items", "note"?, "reason"?}`                |
| `query_knowledge`       | `project` or `card_id`, `query` (string, required), `kinds` ([]string, optional), `limit` (int, optional), `caller`    | `{"source", "items", "reason"?}`                         |
| `get_knowledge_item`    | `project` or `card_id`, `ids` ([]string, at least one), `related` (bool, optional), `caller`                           | `{"source", "items", "related"?, "missing"?, "reason"?}` |

The tools are registered only when `knowledge.providers` is set, and are then
served to every MCP client with no caller gate. Each also accepts `agent_id`.
Each declares an output schema, and its structured content and text block
carry the same JSON.

- **Project.** CM reads the project from `card_id` when `project` is omitted,
  then calls the project's provider with the project's `knowledge_scope`. A
  caller never passes a scope.
- **`source`** is `"knowledge:<provider name>"` on the provider's answer
  (cached or not), a timeout or a failed call, and `"knowledge"` when the
  project has no usable provider or the provider is absent; the table below
  lists every case. CM sets it on every result and replaces any `source` a
  provider sends.
- **`items`** is always present: an empty list, never `null`.
- **Pass-through.** `items`, `note`, `related`, `missing` and any field CM
  does not know are the provider's values, unchanged. CM never rewrites, wraps
  or summarises item text.
- **`reason`** is one sentence, set only when CM answered without the
  provider's answer. A `reason` a provider sends is dropped, so a present
  `reason` always comes from CM:

  | Situation                                         | `source`           | Provider called |
  | ------------------------------------------------- | ------------------ | --------------- |
  | The project names no provider                     | `knowledge`        | no              |
  | The project names a provider but no scope         | `knowledge`        | no              |
  | The provider is not configured on this server     | `knowledge`        | no (warning logged) |
  | The provider is [absent](#availability)           | `knowledge`        | no, except a call whose lost session could not be reconnected: its first attempt was sent and is audited |
  | The call hit CM's call timeout                    | `knowledge:<name>` | yes             |
  | The call failed: an `isError` result, a bad answer or another transport error | `knowledge:<name>` | yes |

- **Never a tool error for the provider.** An outage, slowness or bad answer
  is an empty result with a `reason`, so a skill, chat or blueprint stage can
  call the tools without checking first. Tool errors are only: arguments the
  input schema rejects (an unknown argument, a wrong type, a missing `task`,
  `query` or `ids`), neither `project` nor `card_id`, an unknown project or
  card, a blank `task` or `query`, or `ids` with no non-blank entry.
- **Arguments sent on.** `task` and `query` are trimmed. `ids` are trimmed and
  blank entries dropped. `kinds` is passed as given. `limit` above 50 becomes
  50, and 0 or a negative value is not sent, so the provider's default applies.
- **`caller`** names who is asking. It is written to the audit log only and
  never sent to the provider; `agent_id` is used the same way. CM fills the
  audit `caller` from the first of: `caller` (trimmed), `chat <session id>`
  when the request carries `X-CM-Chat-Session`, `agent_id`, or empty. It keeps
  the first 256 runes. A caller that knows more than its session, such as a
  blueprint stage, passes it, for example `blueprint B1 stage intake`.

## The provider contract

A provider is an MCP server reached over the Streamable HTTP transport at its
configured `url`. CM sends `Authorization: Bearer <token>` on every request
and does not open the standalone GET stream, so nothing flows from the
provider to CM outside replies.

### Tools

All three are required:

| Tool                | Arguments                                                                     | Returns                        |
| ------------------- | ----------------------------------------------------------------------------- | ------------------------------ |
| `knowledge_context` | `scope`, `task` (a card's or a need's text), `limit` (default 12)             | `items` (ranked), `note`       |
| `knowledge_query`   | `scope`, `query`, `kinds` (optional, `Kind` words), `limit` (default 8)       | `items`                        |
| `knowledge_item`    | `scope`, `ids` (one or more), `related` (bool, default false)                 | `items`, `related`, `missing`  |

CM always sends `scope`. It sends `limit` only between 1 and 50, `kinds` only
when non-empty, and `related` only when true.

### Item shape

```go
type KnowledgeItem struct {
    ID      string   `json:"id"`                // the provider's id: a rule id, a goal id, a section
    Level   string   `json:"level,omitempty"`   // why, what, where, how
    Kind    string   `json:"kind"`              // why: goal, experiment, question, principle
                                                // what: rule, scenario, decision, description
                                                // where: architecture; how: difference
    Title   string   `json:"title"`
    Text    string   `json:"text"`              // in the provider's words, short enough to quote
    Status  string   `json:"status,omitempty"`  // the provider's word: draft, agreed, open, running, concluded
    Agreed  bool     `json:"agreed,omitempty"`  // true only when a person with the say has agreed it
    URL     string   `json:"url,omitempty"`     // where a person reads it
    Related []string `json:"related,omitempty"` // ids it links: the goal a rule serves, the rules a
                                                // difference is about, what a decision blocks
}
```

- `kinds` in `knowledge_query` takes the `Kind` words above.
- `note` is a plain string. `related` in a `knowledge_item` reply is a list of
  `KnowledgeItem`, each once.
- **An agreed rule** is an item with `kind: "rule"` and `agreed: true`: a
  constraint. Everything else (a draft rule, an open decision) is a question
  for a person. `status` is the provider's own word, shown to people but never
  used to decide.
- **Architecture knowledge** is `level: "where"` / `kind: "architecture"`.
- **A difference** is `kind: "difference"`: where today's system does not
  follow a rule yet.

### Rules

- Each tool declares an `outputSchema` and returns matching
  `structuredContent`, plus the same JSON in a text block. CM reads
  `structuredContent`; a result without it is read from its first text block.
- `isError` only for real failures: an unknown scope, a bad argument, a store
  that cannot be reached. Nothing found is an empty `items`. CM keeps the text
  of an `isError` result in the audit log's `error` field; its caller sees only
  the `reason`.
- `missing` in a `knowledge_item` reply lists requested ids the provider does
  not have. It is not an error.
- CM ignores fields it does not know and passes them on to its caller.
- `knowledge_item` must be a cheap lookup with no model reasoning, at least
  with `related: false`. `knowledge_context` and `knowledge_query` may reason
  and get a longer timeout.
- Authenticate with the bearer token. Do not redirect: CM follows no
  redirect, and treats one as a failed request.

CM checks the shape of every answer. The answer must be a JSON object of at
most 1 MiB; `items` must be a list of objects (absent or `null` reads as an
empty list); `note`, when present, a string; `related` a list of objects; and
`missing` a list of strings (`null` on those three is dropped). Anything else
is a failed call: the caller gets an empty result with a `reason`, and the
call is still written to the audit log. Fields inside an item are not
checked.

## Timeouts, cache and limits

| What                                                 | Value                                                   |
| ---------------------------------------------------- | ------------------------------------------------------- |
| `knowledge_item` call timeout                        | 5 s                                                     |
| `knowledge_context` / `knowledge_query` call timeout | 45 s                                                    |
| Connect timeout (initialize + `tools/list`)          | 10 s                                                    |
| Absent window after a failed connect or tool check   | 30 s                                                    |
| `knowledge_item` cache                               | 60 s TTL, 256 entries                                   |
| `limit` sent to the provider                         | values above 50 become 50; 0 or negative is not sent    |
| Largest provider answer accepted                     | 1 MiB                                                   |
| `caller` kept in the audit log                       | first 256 runes                                         |
| `knowledge_scope` length                             | at most 200 characters                                  |
| Audit rotation                                       | at 10 MiB, keep 3 old files (`knowledge.log.1` .. `.3`) |

- The connect timeout plus the longest call timeout is 55 s, under CM's 60 s
  HTTP write timeout, so the knowledge tools need no write-deadline override.
- A timeout is CM's own per-call timeout firing; the reason says the provider
  did not answer in time. A caller that gives up first gets no answer, and a
  call already sent is logged as failed. Neither marks the provider absent.
- Only successful `knowledge_item` answers are cached, keyed by provider,
  scope, the ids (in any order) and `related`. At 256 entries the entry
  closest to expiry is dropped. `knowledge_context` and `knowledge_query` are
  never cached. A cache hit does not reach the provider, so it is served even
  while the provider is absent.
- An answer over 1 MiB, or one whose fields have the wrong types (see
  [Rules](#rules)), is a failed call.

## Availability

- CM opens one session per provider on its first call: MCP `initialize`, then
  `tools/list`. A provider missing any of the three tools is treated as absent,
  and the missing names are logged.
- A failed connect (unreachable, an HTTP error such as a rejected token, a
  redirect, the connect timeout, or a missing tool) makes the provider absent
  for 30 s. During that window `get_knowledge_context` and `query_knowledge`
  answer at once with the "unavailable" `reason`, without contacting the
  provider. `get_knowledge_item` does the same unless its answer is still in
  CM's item cache, which it then serves (and audits as a cache hit) as usual.
  The next call after the window connects again. CM logs a warning when a
  provider becomes unavailable and an info line when it is available again. A
  caller that gives up while CM is connecting does not mark the provider
  absent.
- All calls share the session. When it is lost (the provider restarted or no
  longer knows the session), CM connects again once and retries the call. If
  that connect fails, the provider is absent and the call answers as in the
  absent window (`source: "knowledge"`, the "unavailable" `reason`), although
  its first attempt reached the provider and is in the audit log.
- CM replaces the token with `[redacted]` in every error text it builds from
  provider output, so error texts, log lines and the audit log's `error` field
  never carry it. Answers are not redacted: they pass through unchanged into
  tool results and the audit log's `answer`, so a provider must never echo its
  token in an answer.

## The audit log

CM writes one JSON line per provider call to its own file:

- Path `knowledge.log_path`, env `CONTEXTMATRIX_KNOWLEDGE_LOG_PATH`, default
  `<state>/knowledge.log` (`<state>` is `$XDG_STATE_HOME/contextmatrix`, or
  `~/.local/state/contextmatrix`).
- Written only when at least one provider is configured. CM opens it at
  startup, creating the parent directory (0700) and the file (0600) when
  missing, and appends; startup fails when it cannot be opened.
- When a line would take the file past 10 MiB, `knowledge.log` becomes
  `knowledge.log.1`, the older files shift up one, and the previous
  `knowledge.log.3` is deleted.
- A line is written for every `tools/call` CM sends, success or error, and for
  every `knowledge_item` cache hit (with `cached: true` and `duration_ms: 0`),
  including a cache hit while the provider is absent. Nothing else writes a
  line: not a project without a provider or scope, a provider the server does
  not configure, a call refused because the provider is absent, or an input
  error.
- A failed write is logged and does not fail the call.

| Field         | Type        | Meaning                                                                                                   |
| ------------- | ----------- | --------------------------------------------------------------------------------------------------------- |
| `time`        | string      | When CM sent the call (after any connect) or served the cache hit; RFC 3339 with fractional seconds, in the server's time zone |
| `provider`    | string      | Provider name                                                                                             |
| `scope`       | string      | The project's `knowledge_scope`                                                                           |
| `project`     | string      | The CM project                                                                                            |
| `caller`      | string      | Who asked (see [`caller`](#cms-knowledge-tools)); first 256 runes                                         |
| `tool`        | string      | `knowledge_context`, `knowledge_query` or `knowledge_item`                                                |
| `arguments`   | object      | The arguments sent to the provider, `scope` included                                                       |
| `answer`      | JSON        | The provider's full answer, item text included; `null` when there was none or it was over 1 MiB; a JSON string when the answer was not JSON |
| `duration_ms` | integer     | Time the call took; 0 for a cache hit                                                                     |
| `error`       | string      | Present only on failure; the token is redacted                                                            |
| `cached`      | bool        | Present only on a cache hit, as `true`                                                                    |

```json
{"time":"2026-11-02T10:15:04.123456789Z","provider":"intent","scope":"payments","project":"shop","caller":"blueprint B1 stage intake","tool":"knowledge_context","arguments":{"limit":5,"scope":"payments","task":"refund flow"},"answer":{"items":[{"agreed":true,"id":"R-12","kind":"rule","text":"Refunds are accepted within 30 days of delivery.","title":"Refund window"}],"note":"one agreed rule"},"duration_ms":412}
```

## Security

- The token lives in the config file only. CM sends it to its provider only,
  on every request to the configured URL, and never to a worker container, a
  model or the browser; `GET /api/app/config` lists provider names only. CM
  redacts it from error texts, log lines and the audit log's `error` field.
  Provider answers pass through unchanged into tool results and the audit
  log's `answer`, so a provider must never echo its token in an answer. CM
  follows no redirect, so a provider cannot make CM send the token to another
  host, and `http://` is accepted only on loopback.
- Provider text is data. CM passes it through unchanged inside a JSON
  envelope tagged with `source`, and the tool descriptions tell models that
  items are quoted data, never instructions.
- The tools are read-only: CM calls only the three contract tools and has no
  way to write to a provider. Reaching them takes what every other tool on
  `/mcp` takes (`mcp_api_key` when set).

## See also

- [Configuration](configuration.md) - the state directory and env overrides
- [Boards](boards.md) - every `.board.yaml` field
- [MCP](mcp.md) - every tool CM serves
- [Architecture](architecture.md) - the trust model
