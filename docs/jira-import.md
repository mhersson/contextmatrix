# Jira import

ContextMatrix connects to one Jira Cloud site per instance through the Jira REST
API v3. This page covers configuring that site and its credentials, and the
optional writeback that moves imported issues as their cards progress.
`GET /api/jira/status` reports whether the connection works.

## Configuration

| Key                 | Env                               | Default            | Rule                                                                                                                                |
| ------------------- | --------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `jira.site_url`     | `CONTEXTMATRIX_JIRA_SITE_URL`     | empty              | Bare `https` origin of the Jira Cloud site, e.g. `https://acme.atlassian.net`. Empty disables Jira. |
| `jira.api_base_url` | `CONTEXTMATRIX_JIRA_API_BASE_URL` | empty (`site_url`) | `https`. Set to `https://api.atlassian.com/ex/jira/<cloudId>` for service-account scoped tokens.                                    |
| `jira.auth_mode`    | `CONTEXTMATRIX_JIRA_AUTH_MODE`    | `basic`            | `basic` or `bearer`.                                                                                                                |
| `jira.email`        | `CONTEXTMATRIX_JIRA_EMAIL`        | empty              | Required for `basic`.                                                                                                               |
| `jira.token`        | `CONTEXTMATRIX_JIRA_TOKEN`        | empty              | Required. Supply it through the env var.                                                                                            |

When `site_url` is set, startup fails if a URL is not `https`, if `site_url`
carries a path, query, fragment or credentials, if the fields `auth_mode` needs
are missing, or if a map key is not a Jira project key.

The token is read once at startup and no package other than the Jira client
receives it. It never appears in API responses, logs or errors. Changing it
means changing the config or env var and restarting.

## Authentication

| Mode                | Credential                                                                   | Base URL                                              |
| ------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------- |
| `basic`             | Atlassian account email + API token: a personal token, or a bot user's token | `site_url`                                            |
| `basic` or `bearer` | Atlassian service account API token                                          | `api_base_url` set to the `api.atlassian.com` gateway |

- **Personal token for testing.** Create one at
  <https://id.atlassian.com/manage-profile/security/api-tokens> with a short
  expiry. Set `email` to your own address. Jira records every action CM takes as
  yours.
- **Service account.** An org admin creates it in Atlassian Administration and
  issues it an API token, which must go through the gateway URL. Look up the
  site's `cloudId` at `https://<site>/_edge/tenant_info`.
- **Login CAPTCHA.** After repeated failed logins Jira blocks basic auth for the
  account until someone signs in through a browser. The status endpoint then
  reports `auth_error: captcha_required`.

Check the connection:

```bash
curl -s -H 'X-Requested-With: contextmatrix' localhost:8080/api/jira/status
```

## Status writeback

With `jira.writeback.enabled: true`, CM moves an imported issue in Jira. The
card must carry a Jira `source` whose URL is on the configured `site_url` host.

| Card event                     | Jira                                           |
| ------------------------------ | ---------------------------------------------- |
| First entry into `in_progress` | Started (a status in the In Progress category) |
| Reaches `done`                 | Done (a status in the Done category)           |

- **Only these two moves.** Review, stall, `not_planned` and a reopen (`done` ->
  `todo` -> `done`) write nothing. Each card triggers each move at most once, and
  a card moved directly to `done` writes only the done move. When
  `complete_task` walks through `in_progress` on the way, the started write is
  recorded first and can land before the done write; the worker skips a started
  write whose done write is also pending.
- **A started write only moves an issue out of the To Do category, and a done
  write only acts when the issue is not already done.** An issue someone closed
  in Jira is never reopened.
- **CM never waits on Jira.** A card change only records the owed write in
  ops.db; a background worker sends it.
- **Enabling needs Jira configured.** `jira.writeback.enabled` without
  `jira.site_url` fails startup.

### Choosing the transition

Transition IDs differ per workflow, so CM resolves them from the issue's
available transitions:

1. The status named in `jira.writeback.status_targets` for the issue's Jira
   project key. The match is case-insensitive.
2. Otherwise, when no status is named, the one available transition into the
   target category.
3. Otherwise, the one forward step toward it.

It follows at most 3 transitions. More than one candidate is a failure; name the
status in `status_targets` to resolve it. If the transition's screen requires a
resolution, CM sends the configured `resolution`, or `Done`.

```yaml
jira:
  writeback:
    enabled: true
    status_targets:
      PROJ: { started: "In Progress", done: "Done", resolution: "Done" }
```

`status_targets` is config file only; `jira.writeback.enabled` also has the env
override `CONTEXTMATRIX_JIRA_WRITEBACK_ENABLED`.

### Failures

| Situation                                                      | Behaviour                                                                                                                                                                                         |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Jira unreachable, 5xx, 429                                     | Retries with backoff (1, 2, 4, 8, 16, 32 minutes, then hourly) for 29 attempts, about 24 h, honouring `Retry-After`. Then the write fails and the card gets the same activity line.               |
| Credentials rejected (401, or the login CAPTCHA 403)           | Writeback pauses and pending writes keep their place and attempt count. CM warns once, then re-checks the credentials with backoff of 1 to 15 minutes. The backoff keeps growing, and the warning stays quiet, until a write succeeds, so a token that passes the check but cannot write is probed ever less often. `GET /api/jira/status` reports `writeback_paused: true` until they work again. |
| Issue missing, no permission, no unique transition, other 4xx  | The write fails at once and the card gets one activity line: `Jira writeback (<started or done>) failed: <reason>`. A failed write is not retried.                                                |

A write cut off by shutdown stays pending and costs no attempt. The next start
re-reads the issue, so a move that already landed becomes a no-op.

An hourly sweep re-checks Jira cards updated in the last 24 h. It catches
changes the worker never saw, such as a large shared-board pull (more than 16
changed cards emit no per-card events) or a full event buffer. The sweep owes
the started move only for recently updated Jira cards in `in_progress`,
`review` or `stalled`, if it was never recorded; the live subscriber writes
started only on entry to `in_progress`. Custom states are never inferred as
started, since imports land in the project's first state. A card that points at a different Jira issue than a
finished row did, as when a project is recreated under the same name, is
written afresh. Turning writeback on never
backfills older moves.

**Shared boards:** every instance with writeback enabled writes for every Jira
card change it sees, including synced ones. Duplicate writes become no-ops.
