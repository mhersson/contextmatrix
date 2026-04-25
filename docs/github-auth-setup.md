# GitHub authentication setup

ContextMatrix authenticates to GitHub via a single identity used for
both git operations (boards repo, task-skills repo, project repos in
the runner) and REST API calls (issue importing, branch listing).

This guide covers end-to-end setup for both supported methods.

## Choosing a method

| Method | When to use |
|---|---|
| **GitHub App** *(recommended)* | Production deployments. Tokens are short-lived (1h), revocable per installation, and can be scoped finer than a PAT. |
| **Fine-grained PAT** | GitHub Enterprise tenants where App creation is restricted; small-scale or single-developer deployments. |

You can use the same method on the server and the runner, or mix them
(App on the server, PAT on the runner — or vice versa). See
[github-auth-recommended-topologies.md](github-auth-recommended-topologies.md)
for concrete examples.

## What permissions does ContextMatrix need?

| Use case | App permission | Equivalent PAT scope |
|---|---|---|
| Boards repo clone/pull/push | Contents: read & write | Contents: read and write |
| Task-skills repo clone/pull/push | Contents: read & write | Contents: read and write |
| Issue importing (project repos) | Issues: read | Issues: read |
| Branch listing (project repos) | Contents: read | Contents: read |
| Pull request creation (runner) | Pull requests: read & write | Pull requests: read and write |

App-installation tokens automatically include `Metadata: read` — that's
not a separate setting. Fine-grained PAT users have to remember to
include it explicitly.

## Setup: GitHub App

### 1. Create the App

1. Navigate to **Settings → Developer settings → GitHub Apps → New GitHub App** (in your user account or organization, depending on where you want the App to live).
2. Fill in:
   - **GitHub App name**: `contextmatrix-yourorg` (must be globally unique).
   - **Homepage URL**: any URL — required, but unused by ContextMatrix.
   - **Webhook**: uncheck "Active". ContextMatrix doesn't receive webhooks from GitHub directly.
3. Under **Permissions → Repository permissions**, set:
   - **Contents**: read & write
   - **Issues**: read (only if you'll use issue importing)
   - **Pull requests**: read & write (the runner creates PRs)
4. Under **Where can this GitHub App be installed?**, choose **Only on this account** (recommended) or **Any account** if you want to install it on multiple orgs.
5. Click **Create GitHub App**.

### 2. Generate the private key

1. On the App's settings page, scroll to **Private keys**.
2. Click **Generate a private key**. A `.pem` file downloads.
3. Move the file to a secure location (e.g., `/etc/contextmatrix/github-app/private-key.pem` on a single host, or a k8s Secret in production).
4. Note the **App ID** at the top of the App's settings page.

### 3. Install the App on your repos

1. On the App's settings page, click **Install App** in the left sidebar.
2. Choose the account or org and select the repositories the App should access:
   - The **boards repo** (e.g., `contextmatrix-boards`).
   - The **task-skills repo** (e.g., `contextmatrix-task-skills`).
   - Every project repo whose cards ContextMatrix tracks (issue import, branch listing).
3. After installation, the URL shows the **installation ID** as a path segment (e.g., `https://github.com/settings/installations/12345678`).

### 4. Configure ContextMatrix

```yaml
github:
  auth_mode: "app"
  app:
    app_id: 123456                    # from the App's settings page
    installation_id: 12345678         # from the installation URL
    private_key_path: /etc/contextmatrix/github-app/private-key.pem
```

Or via env vars (recommended for production secrets):

```bash
CONTEXTMATRIX_GITHUB_AUTH_MODE=app
CONTEXTMATRIX_GITHUB_APP_ID=123456
CONTEXTMATRIX_GITHUB_INSTALLATION_ID=12345678
CONTEXTMATRIX_GITHUB_PRIVATE_KEY_PATH=/etc/contextmatrix/github-app/private-key.pem
```

The runner takes the same fields with `CMR_GITHUB_*` prefix (see runner README).

### 5. Verify

Start the server. The startup log should show:

```
INFO github token provider initialized auth_mode=app
```

If you see an error like `github api returned status 401`, the App is
not installed on the boards repo (or the installation was not granted
the **Contents: read & write** permission).

## Setup: Fine-grained PAT

### 1. Create the PAT

1. Navigate to **Settings → Developer settings → Personal access tokens → Fine-grained tokens → Generate new token**.
2. Set:
   - **Token name**: `contextmatrix-server` (or `contextmatrix-runner`; use distinct tokens if you want separate audit trails).
   - **Expiration**: as long as your security policy allows (90 days is typical; CM has no in-process refresh, so you'll rotate manually).
   - **Repository access**: **Only select repositories**, then add:
     - The boards repo
     - The task-skills repo
     - Every project repo CM tracks
3. Under **Repository permissions**, grant:
   - **Contents**: Read and write
   - **Issues**: Read (for issue importing)
   - **Metadata**: Read (auto-included; double-check it's there)
   - **Pull requests**: Read and write (the runner creates PRs)
4. Click **Generate token**, copy it (it's shown only once), and store it in your secrets manager.

### 2. Configure ContextMatrix

```yaml
github:
  auth_mode: "pat"
  pat:
    token: ""    # leave empty in YAML; supply via env var below
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
  auth_mode: "app"  # or "pat"
  host: "acme.ghe.com"
  # api_base_url is derived from host: https://api.acme.ghe.com
  app:
    # ...
```

If your enterprise's API URL doesn't match the standard `api.<host>`
pattern, set `github.api_base_url` explicitly.

## Common mistakes

- **PAT created as classic instead of fine-grained.** Classic PATs work
  but give too-broad access and can't be repo-scoped.
- **App not installed on every relevant repo.** Issue import on a repo
  the App isn't installed on returns 404; clone/push on the boards
  repo without installation returns 403. Re-install and pick all the
  repos.
- **Token committed to YAML in a public repo.** Always use env vars
  for secrets in production.
- **Forgetting to renew a PAT.** PATs expire and ContextMatrix has no
  in-process refresh; the day the PAT expires, all GitHub operations
  fail. Apps don't have this problem (the App credentials don't expire;
  only the installation tokens minted from them, which CM mints fresh
  on demand).

## Configuration reference

See `config.yaml.example` for the annotated YAML schema. Every option
above maps to a field in that file.
