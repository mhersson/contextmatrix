# GitHub authentication setup

ContextMatrix (CM) holds every GitHub credential; the execution backends
configure none. The **instance credential** in `github.*` covers CM's own git
operations (boards repo, task-skills repo) and REST calls (issue import, branch
listing), and it is the default source of the tokens CM hands to worker
containers so they can clone and push project repos. In multi-user mode an
admin can add further credentials to an encrypted pool and bind one to a
project with `github_credential` in `.board.yaml`; see
[Per-project credentials](#per-project-credentials).

There is no SSH transport: every remote URL must be `https://`.

## Choosing a method

| Method                         | When to use                                                                                                                                                                                                                      |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **GitHub App** _(recommended)_ | Production. Installation tokens expire within 1h, so the token a worker holds is short-lived; access is revocable per installation and scoped per repo.                                                                          |
| **Fine-grained PAT**           | Tenants where App creation is restricted; single-developer setups. The PAT itself is what workers receive, so it is a long-lived credential on the worker VM. The `await_ci` gate needs Actions: read and Commit statuses: read. |

Either way the credential lives on CM. See
[github-auth-recommended-topologies.md](github-auth-recommended-topologies.md)
for how this maps onto all-in-one, CM-plus-worker-VM, and Kubernetes layouts.

## What permissions does ContextMatrix need?

| Use case                                | Used by                                                                 | App permission              | Fine-grained PAT                                                            |
| --------------------------------------- | ----------------------------------------------------------------------- | --------------------------- | --------------------------------------------------------------------------- |
| Boards repo clone/pull/push             | CM                                                                      | Contents: read & write      | Contents: read and write                                                    |
| Task-skills repo pull                   | CM, and the backends via a CM-minted token                              | Contents: read              | Contents: read                                                              |
| Issue importing (project repos)         | CM                                                                      | Issues: read                | Issues: read                                                                |
| Branch listing (project repos)          | CM                                                                      | Contents: read              | Contents: read                                                              |
| Project repo clone + push               | worker container                                                        | Contents: read & write      | Contents: read and write                                                    |
| Pull request creation                   | worker via `gh` inside the container                                    | Pull requests: read & write | Pull requests: read and write                                               |
| CI checks poll (`await_ci`)             | worker via `gh pr checks`                                               | Checks: read                | Not available; the gate falls back to Actions: read + Commit statuses: read |
| CI failure logs (`await_ci`)            | worker via `gh run view --log-failed`                                   | Actions: read               | Actions: read                                                               |
| Status-API CI results (`await_ci`)      | worker via `gh api .../commits/{sha}/status`                            | Commit statuses: read       | Commit statuses: read                                                       |
| Copilot review (`await_copilot_review`) | worker via `gh api .../pulls/{n}/requested_reviewers` and `.../reviews` | Pull requests: read & write | Pull requests: read and write                                               |

CM never calls GitHub's PR endpoints itself; the worker runs `gh` with the
token CM provisioned for that run. CM's own GitHub traffic is boards sync,
task-skills pull, issue import, and branch listing. `Metadata: read` is
included automatically in App installation tokens; fine-grained PATs must
include it explicitly.

The gate rows apply only to cards that set `await_ci` or
`await_copilot_review`:

- **Checks is App-only.** `gh pr checks` reads the check-run rollup, and
  GitHub grants the Checks permission to Apps only (public repos need no
  permission). On the first "Resource not accessible by personal access
  token" refusal the worker switches, for the rest of the run, to the Actions
  runs API plus the commit-status API, which the PAT permissions above cover.
  GitHub Actions and status-API CI stay fully visible; third-party CI that
  reports only through the Checks API does not.
- **The Copilot gate needs only Pull requests: read & write.** The worker
  requests the reviewer with
  `gh api --method POST repos/{owner}/{repo}/pulls/{n}/requested_reviewers`,
  reads pending requests back from the same endpoint, and reads reviews from
  the REST `pulls/{n}/reviews` endpoints. It does not use
  `gh pr edit --add-reviewer`, so no organization permission is needed. When
  checking by hand use the REST endpoint: `gh pr view --json reviewRequests`
  drops Bot-typed reviewers and never shows Copilot. If the reviewer is still
  unlisted after the request, the gate waits for the review anyway (default
  1200 s, `CMX_GATES_COPILOT_WAIT_TIMEOUT_SECONDS` on the agent backend). A
  422 "Copilot isn't available for this repository" response records the
  reason on the card and passes the gate without waiting. A review that lands
  late is picked up by one more probe after the enabled gates.

## Setup: GitHub App

### 1. Create the App

1. Navigate to **Settings > Developer settings > GitHub Apps > New GitHub App**
   in your user account or organization.
2. Fill in:
   - **GitHub App name**: `contextmatrix-yourorg` (globally unique).
   - **Homepage URL**: any URL; required but unused.
   - **Webhook**: uncheck "Active". CM receives no webhooks from GitHub.
3. Under **Permissions > Repository permissions**, set:
   - **Contents**: read & write
   - **Issues**: read (only for issue importing)
   - **Pull requests**: read & write (PR creation and the Copilot gate)
   - **Checks**: read (only if cards set `await_ci`)
   - **Actions**: read (only if cards set `await_ci`)
   - **Commit statuses**: read (only if cards set `await_ci` and a CI system
     reports through the status API)
4. Leave **Organization permissions** empty.
5. Under **Where can this GitHub App be installed?**, choose **Only on this
   account** unless you install it on several orgs.
6. Click **Create GitHub App**.

Adding permissions to an App that is already installed sends a
permission-update request to each installation; the new permissions take
effect only after you approve it under **Settings > Installations >
Configure**.

### 2. Generate the private key

1. On the App's settings page, scroll to **Private keys** and click
   **Generate a private key**. A `.pem` file downloads.
2. Move it to a secure location on the CM host (for example
   `/etc/contextmatrix/github-app/private-key.pem`) or a Kubernetes Secret.
3. Note the **App ID** at the top of the App's settings page.

### 3. Install the App on your repos

1. On the App's settings page, click **Install App**.
2. Choose the account or org and select the repositories:
   - the **boards repo**
   - the **task-skills repo**, if you use one
   - every project repo CM tracks (issue import, branch listing, and the
     worker tokens all resolve through this installation)
3. After installation, the URL shows the **installation ID** as its last
   path segment (`https://github.com/settings/installations/12345678`).

### 4. Configure ContextMatrix

```yaml
github:
  auth_mode: "app"
  app:
    app_id: 123456 # from the App's settings page
    installation_id: 12345678 # from the installation URL
    private_key_path: /etc/contextmatrix/github-app/private-key.pem
```

Or via env vars (recommended for production secrets):

```bash
CONTEXTMATRIX_GITHUB_AUTH_MODE=app
CONTEXTMATRIX_GITHUB_APP_ID=123456
CONTEXTMATRIX_GITHUB_INSTALLATION_ID=12345678
CONTEXTMATRIX_GITHUB_PRIVATE_KEY_PATH=/etc/contextmatrix/github-app/private-key.pem
```

This is the only place the credential is configured. The agent backend
receives a token in every trigger and refreshes it from
`GET /api/agent/git-credentials`; chat workers fetch one from
`GET /api/worker/git-credentials`. Neither backend has a `github:` block.

### 5. Verify

Start the server. The startup log should show:

```
INFO github token provider initialized auth_mode=app
```

Config validation only checks that `private_key_path` is non-empty, but the
PEM is read and parsed right after, so a missing or malformed key file fails
startup with `failed to construct github token provider`. Installation and
permission problems surface on the first GitHub call instead: `github api:
status 401` or `status 404` from a clone or REST call means the App is not
installed on that repo or lacks the permission.

## Setup: Fine-grained PAT

### 1. Create the PAT

1. Navigate to **Settings > Developer settings > Personal access tokens >
   Fine-grained tokens > Generate new token**.
2. Set:
   - **Token name**: `contextmatrix`.
   - **Expiration**: as long as policy allows. CM has no in-process refresh,
     so rotation is manual.
   - **Repository access**: **Only select repositories**: the boards repo,
     the task-skills repo, and every project repo CM tracks.
3. Under **Repository permissions**, grant:
   - **Contents**: Read and write
   - **Issues**: Read (for issue importing)
   - **Metadata**: Read (auto-included; check it is there)
   - **Pull requests**: Read and write (PR creation and the Copilot gate)
   - **Actions**: Read (only if cards set `await_ci`)
   - **Commit statuses**: Read (only if cards set `await_ci` and a CI system
     reports through the status API)
4. Leave **Organization permissions** empty.
5. Click **Generate token**, copy it (shown once), and store it in your
   secrets manager.

### 2. Configure ContextMatrix

```yaml
github:
  auth_mode: "pat"
  pat:
    token: "" # leave empty in YAML; supply via env var below
```

```bash
CONTEXTMATRIX_GITHUB_PAT_TOKEN=github_pat_xxxxxxxxxxxxxxxxxxxxxx
```

`app.*` must be empty in PAT mode and `pat.token` must be empty in App mode;
validation rejects a mix.

### 3. Verify

```
INFO github token provider initialized auth_mode=pat
```

## Per-project credentials

In `auth.mode: multi` an admin can register additional credentials in the
instance pool (`/api/admin/credentials`, or the admin UI). Each entry has a
`name`, a `kind` (`pat` or `app`), an optional `host` and `api_base_url`, and
for Apps an `app_id`, `installation_id`, and the PEM as the secret. CM checks
the credential against GitHub before saving it and stores the secret encrypted
under the auth master key. A project binds one with `github_credential: <name>`
in `.board.yaml`; CM then uses that credential for the project's issue import,
branch listing, and worker tokens. A broken or disabled binding fails closed
and never falls back to the instance credential. Pool entries follow the same
permission table as the instance credential. See
[authentication.md](authentication.md).

## Issue importing

Enable the importer with `github.issue_importing.enabled: true` and
`sync_interval` (minimum and default `5m`); per-project settings live under
`github:` in `.board.yaml`. See
[github-issue-import.md](github-issue-import.md).

## GitHub Enterprise (GHEC-DR / GHES)

Set `github.host` to your enterprise hostname (no scheme):

```yaml
github:
  auth_mode: "app" # or "pat"
  host: "acme.ghe.com"
  # api_base_url defaults to https://api.acme.ghe.com when left blank.
  app:
    # ...
```

If the enterprise API URL does not follow the `api.<host>` pattern, set
`github.api_base_url` explicitly. When `github.host` is set, both `github.com`
and the enterprise host are accepted for repo URLs (boards, task-skills, and
every project `repo`), so one instance can coordinate both with one identity,
provided the App or PAT has access on both.

## Common mistakes

- **Classic PAT instead of fine-grained.** Classic PATs work but cannot be
  repo-scoped.
- **`await_ci` under PAT auth without Actions: read.** The fallback poll reads
  the Actions runs API; without it (or without Commit statuses: read for
  status-API CI) every poll fails until the gate's wait cap expires and the
  card parks.
- **App permissions added but never approved on the installation.** Until the
  permission-update request is approved, the gates fail exactly as if the
  permission were missing.
- **App not installed on every relevant repo.** Issue import on such a repo
  returns 404; clone or push returns 403; a worker token for that repo fails
  the same way.
- **Token committed to YAML.** Use env vars for secrets in production.
- **Expired PAT.** The day it expires every GitHub operation fails, including
  the tokens CM hands to workers. App credentials do not expire; CM caches
  each installation token and mints a new one when it is within five minutes
  of expiry.

## Configuration reference

`config.yaml.example` documents every `github.*` key and its
`CONTEXTMATRIX_GITHUB_*` override; see also [configuration.md](configuration.md).
