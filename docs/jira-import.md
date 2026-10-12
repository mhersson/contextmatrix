# Jira import

A human imports a Jira Cloud epic from the "New playbook" form. Each selected
child issue and sub-task becomes a card in an existing project; the epic becomes
a playbook that orders those cards, ready to be made runnable. The import is a
one-shot snapshot: ContextMatrix is the source of truth afterwards and nothing
syncs back in. ContextMatrix talks to one Jira Cloud site per instance through
the REST API v3; without a configured site the import button is hidden.
`GET /api/jira/status` reports whether the connection works.

## Configuration

| Key                 | Env                               | Default            | Rule                                                                                                                                |
| ------------------- | --------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `jira.site_url`     | `CONTEXTMATRIX_JIRA_SITE_URL`     | empty              | Bare `https` origin of the Jira Cloud site, e.g. `https://acme.atlassian.net`. Empty disables Jira. |
| `jira.api_base_url` | `CONTEXTMATRIX_JIRA_API_BASE_URL` | empty (`site_url`) | `https`. Set to `https://api.atlassian.com/ex/jira/<cloudId>` for service-account scoped tokens.                                    |
| `jira.auth_mode`    | `CONTEXTMATRIX_JIRA_AUTH_MODE`    | `basic`            | `basic` or `bearer`.                                                                                                                |
| `jira.email`        | `CONTEXTMATRIX_JIRA_EMAIL`        | empty              | Required for `basic`.                                                                                                               |
| `jira.token`        | `CONTEXTMATRIX_JIRA_TOKEN`        | empty              | Required. Supply it through the env var.                                                                                            |
| `jira.project_map`  | none (a map; set it in the file)  | empty              | Wizard defaults per Jira project key: `default` names a CM project, `components` maps a Jira component to one and wins over `default`. A target that names no CM project is logged at startup and ignored. |

When `site_url` is set, startup fails if a URL is not `https`, if `site_url`
carries a path, query, fragment or credentials, if the fields `auth_mode` needs
are missing, if a `project_map` key is not a Jira project key, or if a
`project_map` entry has neither a `default` nor `components`.

```yaml
jira:
  project_map:
    PROJ:
      default: api
      components:
        Frontend: web
```

The map only fills the import dialog's project pickers; a human can change every
pick. For an issue with several components, the first one that maps to an
existing CM project wins. Sub-tasks default to their parent's project.

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

## Importing an epic

1. Click **New playbook**, paste an epic key (`PROJ-1`, any case) or a Jira URL
   on the configured site into the title, and click **Import from Jira**. Enter
   still creates an ordinary playbook. While the token fails, the form says why
   (`unauthorized`, `captcha_required`, `unreachable`) and the button stays
   disabled.
2. The dialog shows the epic's children and their sub-tasks. Open issues that
   were never imported are preselected. Pick a project per issue, or one for
   all; defaults come from `jira.project_map`, and sub-tasks follow their
   parent.
3. Choose whether the cards are vetted and whether each card carries the epic
   description, then import. The dialog lists each issue's result as it lands
   (`created`, `skipped`, `failed`), then offers **Open playbook**.

The import re-reads every issue from Jira before it answers; the browser sends
only keys, projects and options. It then runs in the background, bounded at 10
minutes, and reports each issue over the event stream; closing the dialog does
not stop it, and a reconnecting dialog catches up from
`GET /api/jira/imports/{id}`, which keeps a finished import for an hour on the
instance that ran it. Imports of the same epic run one at a time on an instance.
The import is human-only.

At server shutdown, or after the 10 minute bound, an import stops between two
issues: the issue in flight finishes, the import is marked `failed`, and the
issues it never reached are listed as not run.

## What a card gets

| Field                     | Value                                                                                                                                                                                                                                                      |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `title`                   | `PROJ-123 <summary>`, with every `#` run directly before a letter or digit removed so the merge commit cannot run a Jira smart-commit command                                                                                                              |
| `body`                    | The issue description as markdown. With the epic option, followed by `## Epic context ([PROJ-1](<epic URL>))` and the epic description; nothing is added when the epic has none                                                                            |
| `type`                    | `Bug` -> `bug`; `Improvement`, `New Feature`, `Feature` -> `feature` when the project lists it; anything else, and every sub-task, -> `task`. A type the project does not list falls back to its first type                                               |
| `priority`                | `Highest`, `Critical`, `Blocker` -> `critical`; `High` -> `high`; `Medium`, `Normal` -> `medium`; `Low`, `Lowest`, `Trivial`, `Minor` -> `low`; unknown -> `medium`. A missing priority falls back `critical` -> `high` -> `medium` -> the project's first |
| `source`                  | `{system: jira, external_id: PROJ-123, external_url: <site>/browse/PROJ-123}`, immutable                                                                                                                                                                   |
| `custom.jira_issue_id`    | The Jira issue id                                                                                                                                                                                                                                          |
| `custom.jira_import_hash` | Hex SHA-256 of the imported title, a newline and the body                                                                                                                                                                                                  |
| `vetted`                  | The dialog's **Mark imported cards vetted** (off by default)                                                                                                                                                                                               |
| `branch_name`             | `<id>/PROJ-123-<slug>`, key uppercase, so GitHub for Jira links the branch (a key over 64 bytes keeps the plain name)                                                                                                                                      |
| activity log              | `Imported from Jira PROJ-123` (with `, marked vetted on import` when vetted), attributed to the importing human                                                                                                                                            |

Jira children never become CM subtasks: `parent` stays empty and the type is
never `subtask`. Subtasks come only from CM's own planning.

## Order and dependencies

- Sub-tasks are created before their parent and sit before it in the playbook.
- A parent `depends_on` its sub-tasks that land in the same project. A sub-task
  in another project is order only; the dialog warns about it.
- A new sub-task of a parent imported earlier goes directly before the parent in
  the playbook. Existing cards are never edited, so the parent does not gain the
  dependency.
- A sub-task moved to `not_planned` keeps its same-project parent blocked:
  dependencies need `done`. Remove the dependency by hand.

## Re-running

An issue already imported (matched on `custom.jira_issue_id`, then on
`source.external_id` when the id agrees) is `skipped`, never duplicated, and is
put back into the playbook if it is missing there. If that playbook is runnable,
the card gets the playbook's forced card settings, as any card added to a
runnable playbook does. The epic's playbook is found by its `source` in any
boards repository; the title and repository typed in the form apply only when
it is created. A failed row never blocks the others; fix the cause and import
again. A server shutdown stops a running import between two issues and marks it
`failed`; running it again finishes the job.

## Active runs

An import into an epic whose playbook has a `running` or `waiting` run is
refused with `409 JIRA_PLAYBOOK_RUNNING`. The preview flags it up front
(`run_active`) and the dialog says to stop the run or wait.

If a run starts while an import is in progress, new cards are still added to the
playbook but stay at the end: entry order is frozen while a run is active
(`409 PLAYBOOK_MOVE_DURING_RUN`), so they are not moved before their parent.

## Vetting

Imported cards are unvetted unless the box is ticked. An unvetted card cannot be
claimed by an agent or launched, and making the playbook runnable is refused
until its open imported cards are vetted (see
[playbooks](playbooks.md#make-runnable)). Unvetted imported cards can still be
added to a playbook that is already runnable: the run then waits on each one
until a human vets it. Re-asserting runnable on an already-runnable playbook
(`PATCH runnable:true`) answers `422 PLAYBOOK_CARD_NOT_VETTED` until they are
vetted, as the first make-runnable does. The epic context sits in the card body,
so vetting covers it.

## Pull requests and branches

Every card in a runnable playbook keeps its own agent branch and opens a PR into
`playbook/<id>` (see [playbooks](playbooks.md#the-branch-model)). The PR title
is the card title, so it starts with the issue key and GitHub for Jira links it.

When a target project's card prefix equals a Jira project key (a CM project with
prefix `PROJ` receiving `PROJ-...` issues), a card ID such as `PROJ-801` in a PR
body or commit also reads as a Jira key, and GitHub for Jira may link the PR to
the wrong issue. The import dialog warns on such issues but never blocks the
import; pick another project or change the prefix to avoid it.

## Import errors

| Situation                                                       | Response                                                                                                     |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Malformed key or body, no selections, or more than 200          | `400 BAD_REQUEST`                                                                                            |
| Import called with an agent identity                            | `403 HUMAN_ONLY_FIELD`                                                                                       |
| Jira not configured, or playbooks disabled                      | `404 JIRA_NOT_CONFIGURED`; the button is hidden                                                              |
| Not an epic (hierarchy level 1)                                 | `422 JIRA_NOT_EPIC`                                                                                          |
| Issue missing or no permission                                  | `404 JIRA_ISSUE_NOT_FOUND`                                                                                   |
| The epic's playbook has a `running` or `waiting` run            | `409 JIRA_PLAYBOOK_RUNNING`; stop the run or wait                                                            |
| Token rejected                                                  | `502 JIRA_AUTH_FAILED`; `details` names a login CAPTCHA when Jira reports one                                |
| Jira refused the request with another 4xx                       | `502 JIRA_REQUEST_REJECTED` with Jira's message                                                              |
| Jira rate limit outlasts the wait                               | `503 JIRA_RATE_LIMITED`; nothing was written                                                                 |
| Server not accepting imports, as during shutdown                | `503 JIRA_IMPORT_UNAVAILABLE`; try again shortly                                                             |
| Jira down                                                       | `502 JIRA_UNAVAILABLE`                                                                                       |
| Import state asked for after it aged out or on another instance | `404 JIRA_IMPORT_NOT_FOUND`                                                                                  |
| A large import on a shared boards repo                          | No proxy timeout: the request answers `202` once the epic is read and progress arrives over the event stream |

Endpoints and payloads: [API reference](api-reference.md#jira).
