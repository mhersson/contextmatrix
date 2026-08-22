# GitHub authentication setup

ContextMatrix authenticates to GitHub via a **single identity held by CM
only**. That identity covers CM's own git operations (boards repo, task-skills
repo) and REST calls (issue import, branch listing), and it is the source of the
**short-lived, per-run tokens CM mints for worker containers** so they can clone
and push project repos. Workers never hold a long-lived credential; the backends
never configure their own.

This guide covers end-to-end setup for both supported methods.

## Choosing a method

| Method                         | When to use                                                                                                          |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------- |
| **GitHub App** _(recommended)_ | Production deployments. Tokens are short-lived (1h), revocable per installation, and can be scoped finer than a PAT. |
| **Fine-grained PAT**           | GitHub Enterprise tenants where App creation is restricted; small-scale or single-developer deployments. The `await_ci` gate works via an automatic Actions-API fallback - grant Actions: read and Commit statuses: read. |

The credential lives on CM regardless of method. See
[github-auth-recommended-topologies.md](github-auth-recommended-topologies.md)
for how this maps onto all-in-one, CM-plus-worker-VM, and Kubernetes layouts.

## What permissions does ContextMatrix need?

| Use case                                     | Used by                                        | App permission              | Equivalent PAT scope          |
| -------------------------------------------- | ---------------------------------------------- | --------------------------- | ----------------------------- |
| Boards repo clone/pull/push                  | CM                                             | Contents: read & write      | Contents: read and write      |
| Task-skills repo clone/pull                  | CM (backends clone via a CM-minted token)      | Contents: read & write      | Contents: read and write      |
| Issue importing (project repos)              | CM                                             | Issues: read                | Issues: read                  |
| Branch listing (project repos)               | CM                                             | Contents: read              | Contents: read                |
| Project repo clone + push (worker container) | worker (via a per-run token CM mints)          | Contents: read & write      | Contents: read and write      |
| Pull request creation (project repos)        | worker via `gh`/model inside the container     | Pull requests: read & write | Pull requests: read and write |
| CI checks poll (`await_ci` gate)             | worker via `gh pr checks`                      | Checks: read                | Not available - gate falls back to Actions: read + Commit statuses: read |
| CI failure logs (`await_ci` gate)            | worker via `gh run view --log-failed`          | Actions: read               | Actions: read                 |
| Status-API CI results (non-Actions CI)       | worker via `gh pr checks`                      | Commit statuses: read       | Commit statuses: read         |
| Copilot reviewer request + review read (`await_copilot_review` gate) | worker via `gh api repos/{o}/{r}/pulls/{n}/requested_reviewers` and `.../reviews` | Pull requests: read & write | Pull requests: read and write |

CM does not call GitHub's PR-creation endpoint itself - the worker container runs
`gh pr create` (or equivalent) using the token CM minted for that run. CM's own
direct GitHub traffic is boards sync, task-skills pull, issue import, and branch
listing.

The four PR-gate rows apply only to cards that set `await_ci` or
`await_copilot_review`. Two of them need attention:

- **Checks is App-only - the gate falls back under PAT auth.** `gh pr
  checks` reads check-run results through the GraphQL `statusCheckRollup`
  field, and GitHub grants the Checks permission to GitHub Apps only - the
  fine-grained PAT UI does not offer it (public repos are exempt: check-run
  reads there need no permission). When the poll hits "Resource not
  accessible by personal access token", the worker switches for the rest of
  the run to the Actions runs API plus the legacy commit-status API, which
  the PAT permissions above cover. GitHub Actions CI and status-API CI are
  fully visible in fallback mode; only third-party integrations that report
  exclusively through the Checks API are not.
- **The Copilot gate needs no permission beyond Pull requests: read and
  write.** The worker requests the reviewer with
  `gh api --method POST repos/{owner}/{repo}/pulls/{n}/requested_reviewers`,
  reads pending requests back from the same endpoint, and reads reviews and
  review comments from the REST `pulls/{n}/reviews` endpoints - all covered
  by the PR-creation row above. It does not use `gh pr edit
  --add-reviewer`, so the organization-level Members permission is not
  needed. Community reports indicate requesting a Copilot review works with
  user-scoped fine-grained PATs but not with App installation tokens in some
  setups; if the Copilot gate logs a request failure under App auth, that
  asymmetry is the likely cause. When checking by hand, use
  `gh api repos/{owner}/{repo}/pulls/{n}/requested_reviewers` - gh's
  `pr view --json reviewRequests` drops Bot-typed reviewers and never shows
  Copilot, which is why the worker reads REST. Gate behaviour: if the
  reviewer is still unlisted after the request, the gate waits for the
  review anyway (default 20 minutes,
  `CMX_GATES_COPILOT_WAIT_TIMEOUT_SECONDS`); only a 422 "Copilot isn't
  available for this repository" response records the reason on the card
  and passes the gate without waiting; a review that lands late is picked
  up by one more check after the CI gate.

App-installation tokens automatically include `Metadata: read` - that's not a
separate setting. Fine-grained PAT users have to remember to include it
explicitly.

## Setup: GitHub App

### 1. Create the App

1. Navigate to **Settings → Developer settings → GitHub Apps → New GitHub App**
   (in your user account or organization, depending on where you want the App to
   live).
2. Fill in:
   - **GitHub App name**: `contextmatrix-yourorg` (must be globally unique).
   - **Homepage URL**: any URL - required, but unused by ContextMatrix.
   - **Webhook**: uncheck "Active". ContextMatrix doesn't receive webhooks from
     GitHub directly.
3. Under **Permissions → Repository permissions**, set:
   - **Contents**: read & write
   - **Issues**: read (only if you'll use issue importing)
   - **Pull requests**: read & write (worker containers create PRs and,
     for `await_copilot_review`, request and read Copilot reviews)
   - **Checks**: read (only if cards set `await_ci` - `gh pr checks` reads
     check-run results)
   - **Actions**: read (only if cards set `await_ci` - `gh run view
     --log-failed` fetches failing-run logs)
   - **Commit statuses**: read (only if cards set `await_ci` and a CI system
     reports through the legacy status API)
4. Leave **Permissions → Organization permissions** empty - the
   `await_copilot_review` gate needs only Pull requests: read & write.
5. Under **Where can this GitHub App be installed?**, choose **Only on this
   account** (recommended) or **Any account** if you want to install it on
   multiple orgs.
6. Click **Create GitHub App**.

Adding permissions to an App that is already installed sends a
permission-update request to each installation; the new permissions take
effect only after you approve it under **Settings → Installations →
Configure**.

### 2. Generate the private key

1. On the App's settings page, scroll to **Private keys**.
2. Click **Generate a private key**. A `.pem` file downloads.
3. Move the file to a secure location (e.g.,
   `/etc/contextmatrix/github-app/private-key.pem` on a single host, or a k8s
   Secret in production). It lives on CM only.
4. Note the **App ID** at the top of the App's settings page.

### 3. Install the App on your repos

1. On the App's settings page, click **Install App** in the left sidebar.
2. Choose the account or org and select the repositories the App should access:
   - The **boards repo** (e.g., `contextmatrix-boards`).
   - The **task-skills repo** (e.g., `contextmatrix-task-skills`).
   - Every project repo whose cards ContextMatrix tracks - needed both for
     CM's issue import / branch listing and for the per-run tokens CM mints so
     workers can clone and push.
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

This is the only place the GitHub credential is configured. The agent and chat
backends receive minted tokens from CM at run time (agent workers via
`GET /api/agent/git-credentials`, chat workers via
`GET /api/worker/git-credentials`); they have no `github:` block of their own.

### 5. Verify

Start the server. The startup log should show:

```
INFO github token provider initialized auth_mode=app
```

The server does not validate the private-key file at config-load - it only
checks that `private_key_path` is non-empty. A missing or unreadable PEM file
fails on the first GitHub call (e.g., `git clone` of the boards repo) rather
than at startup.

If you see `github api: status 401` or `status 404` from a git clone or REST
call, the App is not installed on the relevant repo (or the installation was not
granted the **Contents: read & write** permission).

## Setup: Fine-grained PAT

### 1. Create the PAT

1. Navigate to **Settings → Developer settings → Personal access tokens →
   Fine-grained tokens → Generate new token**.
2. Set:
   - **Token name**: `contextmatrix`.
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
   - **Pull requests**: Read and write (worker containers create PRs and,
     for `await_copilot_review`, request and read Copilot reviews)
   - **Actions**: Read (only if cards set `await_ci` - CI polling fallback and
     failing-run logs)
   - **Commit statuses**: Read (only if cards set `await_ci` and a CI system
     reports through the legacy status API)
4. Leave **Organization permissions** (shown when the token's resource owner
   is an organization) empty - the `await_copilot_review` gate needs only
   Pull requests: Read and write.
5. Click **Generate token**, copy it (it's shown only once), and store it in
   your secrets manager.

**The `await_ci` gate under a fine-grained PAT** polls through an automatic
fallback: the Checks API is App-only, so on the first "Resource not
accessible by personal access token" refusal the worker reads the Actions
runs API and the legacy commit-status API instead. Grant **Actions: read**
and **Commit statuses: read** (both listed above) and the gate behaves
identically for GitHub Actions and status-API CI. Third-party CI that
reports only through the Checks API is invisible to a PAT on private repos.

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
  # api_base_url is derived from host as https://api.acme.ghe.com when left blank.
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

## Common mistakes

- **PAT created as classic instead of fine-grained.** Classic PATs work but give
  too-broad access and can't be repo-scoped.
- **`await_ci` under PAT auth without Actions: read.** The fallback poll
  reads the Actions runs API; a PAT missing **Actions: read** (or **Commit
  statuses: read** for status-API CI) leaves every poll failing until the
  gate's wait cap expires and the card parks. Workers older than the
  fallback also park this way regardless of permissions - update the worker
  image.
- **App permissions added but never approved on the installation.** Granting
  Checks/Actions/Commit statuses to an existing App has no effect until the
  permission-update request is approved under **Settings → Installations →
  Configure**; until then the gates fail exactly as if the permission were
  missing.
- **App not installed on every relevant repo.** Issue import on a repo the App
  isn't installed on returns 404; clone/push on the boards repo without
  installation returns 403; a worker's per-run token for a project repo fails
  the same way. Re-install and pick all the repos.
- **Token committed to YAML in a public repo.** Always use env vars for secrets
  in production.
- **Forgetting to renew a PAT.** PATs expire and ContextMatrix has no in-process
  refresh; the day the PAT expires, all GitHub operations fail - including the
  worker tokens CM mints from it. Apps don't have this problem: the App
  credentials (App ID + private key) don't expire, only the installation tokens
  minted from them. CM wraps its provider in a caching layer that reuses
  installation tokens until they near expiry, and mints the fresh, short-lived
  per-run/per-repo tokens it hands to workers from that same identity.

## Configuration reference

See `config.yaml.example` for the annotated YAML schema. Every option above maps
to a field in that file.
