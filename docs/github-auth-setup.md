# GitHub authentication setup

ContextMatrix authenticates to GitHub via a single identity used for both git
operations (boards repo, task-skills repo, project repos in the runner) and REST
API calls (issue importing, branch listing).

This guide covers end-to-end setup for both supported methods.

## Choosing a method

| Method                         | When to use                                                                                                          |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------- |
| **GitHub App** _(recommended)_ | Production deployments. Tokens are short-lived (1h), revocable per installation, and can be scoped finer than a PAT. |
| **Fine-grained PAT**           | GitHub Enterprise tenants where App creation is restricted; small-scale or single-developer deployments.             |

You can use the same method on the server and the runner, or mix them (App on
the server, PAT on the runner — or vice versa). See
[github-auth-recommended-topologies.md](github-auth-recommended-topologies.md)
for concrete examples.

## What permissions does ContextMatrix need?

| Use case                                     | Used by                                      | App permission              | Equivalent PAT scope          |
| -------------------------------------------- | -------------------------------------------- | --------------------------- | ----------------------------- |
| Boards repo clone/pull/push                  | server                                       | Contents: read & write      | Contents: read and write      |
| Task-skills repo clone/pull/push             | server, runner (pre-spawn pull)              | Contents: read & write      | Contents: read and write      |
| Issue importing (project repos)              | server                                       | Issues: read                | Issues: read                  |
| Branch listing (project repos)               | server                                       | Contents: read              | Contents: read                |
| Project repo clone + push (worker container) | worker (via `CM_GIT_TOKEN` minted by runner) | Contents: read & write      | Contents: read and write      |
| Pull request creation (project repos)        | worker via `gh`/Claude inside the container  | Pull requests: read & write | Pull requests: read and write |

The runner itself does not call any GitHub REST endpoint for PR creation — it
mints a token and hands it to the worker container, which runs `gh pr create`
(or equivalent) using `CM_GIT_TOKEN`. The runner's only direct GitHub
interaction is pulling the task-skills repo before spawning a worker.

App-installation tokens automatically include `Metadata: read` — that's not a
separate setting. Fine-grained PAT users have to remember to include it
explicitly.

## Setup: GitHub App

### 1. Create the App

1. Navigate to **Settings → Developer settings → GitHub Apps → New GitHub App**
   (in your user account or organization, depending on where you want the App to
   live).
2. Fill in:
   - **GitHub App name**: `contextmatrix-yourorg` (must be globally unique).
   - **Homepage URL**: any URL — required, but unused by ContextMatrix.
   - **Webhook**: uncheck "Active". ContextMatrix doesn't receive webhooks from
     GitHub directly.
3. Under **Permissions → Repository permissions**, set:
   - **Contents**: read & write
   - **Issues**: read (only if you'll use issue importing)
   - **Pull requests**: read & write (the runner creates PRs)
4. Under **Where can this GitHub App be installed?**, choose **Only on this
   account** (recommended) or **Any account** if you want to install it on
   multiple orgs.
5. Click **Create GitHub App**.

### 2. Generate the private key

1. On the App's settings page, scroll to **Private keys**.
2. Click **Generate a private key**. A `.pem` file downloads.
3. Move the file to a secure location (e.g.,
   `/etc/contextmatrix/github-app/private-key.pem` on a single host, or a k8s
   Secret in production).
4. Note the **App ID** at the top of the App's settings page.

### 3. Install the App on your repos

1. On the App's settings page, click **Install App** in the left sidebar.
2. Choose the account or org and select the repositories the App should access:
   - The **boards repo** (e.g., `contextmatrix-boards`).
   - The **task-skills repo** (e.g., `contextmatrix-task-skills`).
   - Every project repo whose cards ContextMatrix tracks (issue import, branch
     listing).
3. After installation, the URL shows the **installation ID** as a path segment
   (e.g., `https://github.com/settings/installations/12345678`).

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

The runner takes the same auth-mode fields (`auth_mode`, `app_id`,
`installation_id`, `private_key_path`, `pat.token`, `host`, `api_base_url`) with
the `CMR_GITHUB_*` env-var prefix — see the
[contextmatrix-runner README](https://github.com/mhersson/contextmatrix-runner).
Issue importing is server-only: there is no `CMR_GITHUB_ISSUE_IMPORTING_*`
equivalent and the runner's `GitHubConfig` has no `IssueImporting` field.

### 5. Verify

Start the server. The startup log should show:

```
INFO github token provider initialized auth_mode=app
```

The server does not validate the private-key file at config-load — it only
checks that `private_key_path` is non-empty. A missing or unreadable PEM file
fails on the first GitHub call (e.g., `git clone` of the boards repo) rather
than at startup. The runner is stricter: it `os.Stat`s `private_key_path` in
`Validate()` and refuses to start if the file is missing.

If you see `github api: status 401` or `status 404` from a git clone or REST
call, the App is not installed on the relevant repo (or the installation was not
granted the **Contents: read & write** permission).

## Setup: Fine-grained PAT

### 1. Create the PAT

1. Navigate to **Settings → Developer settings → Personal access tokens →
   Fine-grained tokens → Generate new token**.
2. Set:
   - **Token name**: `contextmatrix-server` (or `contextmatrix-runner`; use
     distinct tokens if you want separate audit trails).
   - **Expiration**: as long as your security policy allows (90 days is typical;
     CM has no in-process refresh, so you'll rotate manually).
   - **Repository access**: **Only select repositories**, then add:
     - The boards repo
     - The task-skills repo
     - Every project repo CM tracks
3. Under **Repository permissions**, grant:
   - **Contents**: Read and write
   - **Issues**: Read (for issue importing)
   - **Metadata**: Read (auto-included; double-check it's there)
   - **Pull requests**: Read and write (the runner creates PRs)
4. Click **Generate token**, copy it (it's shown only once), and store it in
   your secrets manager.

### 2. Configure ContextMatrix

```yaml
github:
  auth_mode: "pat"
  pat:
    token: "" # leave empty in YAML; supply via env var below
```

Env var:

```bash
CONTEXTMATRIX_GITHUB_PAT_TOKEN=github_pat_xxxxxxxxxxxxxxxxxxxxxx
```

### 3. Verify

```
INFO github token provider initialized auth_mode=pat
```

## GitHub Enterprise (GHEC-DR / GHES)

Set `github.host` to your enterprise hostname (no scheme):

```yaml
github:
  auth_mode: "app" # or "pat"
  host: "acme.ghe.com"
  # On the server, api_base_url is derived from host as
  # https://api.acme.ghe.com when left blank.
  app:
    # ...
```

If your enterprise's API URL doesn't match the standard `api.<host>` pattern,
set `github.api_base_url` explicitly.

When `github.host` is set, both `github.com` and the enterprise hostname are
accepted simultaneously for project repo URLs (boards, task-skills, and any
project's `repo` field in `.board.yaml`). This lets a single CM instance
coordinate work across both surfaces with one identity, provided the App or PAT
has access on both. See `internal/config/config.go` (`AllowedHosts`).

Note: `api_base_url` derivation is server-only. The contextmatrix-runner does
**not** derive `api_base_url` from `host`; if you set `host` on the runner you
must also set `api_base_url` (or `CMR_GITHUB_API_BASE_URL`) explicitly,
otherwise the runner passes an empty value to the GitHub auth provider.

## Common mistakes

- **PAT created as classic instead of fine-grained.** Classic PATs work but give
  too-broad access and can't be repo-scoped.
- **App not installed on every relevant repo.** Issue import on a repo the App
  isn't installed on returns 404; clone/push on the boards repo without
  installation returns 403. Re-install and pick all the repos.
- **Token committed to YAML in a public repo.** Always use env vars for secrets
  in production.
- **Forgetting to renew a PAT.** PATs expire and ContextMatrix has no in-process
  refresh; the day the PAT expires, all GitHub operations fail. Apps don't have
  this problem: the App credentials (App ID + private key) don't expire, only
  the installation tokens minted from them. The server wraps its provider in a
  caching layer so it reuses installation tokens until they near expiry; the
  runner deliberately does **not** cache and mints a fresh installation token
  per worker spawn (so the token handed off to the long-lived worker container
  is as far from expiry as possible).

## Configuration reference

See `config.yaml.example` for the annotated YAML schema. Every option above maps
to a field in that file.
