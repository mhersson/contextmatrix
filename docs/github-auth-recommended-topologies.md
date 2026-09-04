# Recommended GitHub auth topologies

The GitHub credential lives on **ContextMatrix (CM) only**, whatever the
layout. CM uses it for boards sync, task-skills pull, issue import, and branch
listing, and it hands worker containers the token they use to clone and push
project repos: in App mode a short-lived installation token, in PAT mode the
PAT itself. The agent and chat backends carry no GitHub credential of their
own. For App and PAT creation, permissions, and the per-project credential
pool, see [github-auth-setup.md](github-auth-setup.md).

"Topology" is therefore about where CM runs and how the backends reach it,
not about distributing GitHub identities.

**Which component touches which repo:**

| Repo                   | ContextMatrix                                         | Backends / worker containers                                     |
| ---------------------- | ----------------------------------------------------- | ---------------------------------------------------------------- |
| Boards repo            | clone / pull / push                                   | not accessed                                                     |
| Task-skills repo       | pull; serves a `{git_remote_url, ref, token}` pointer | backend clones the pointer with the token CM minted              |
| Project repos (GitHub) | issue import (REST), branch list (REST)               | worker clones / pushes / opens PRs with the token CM provisioned |

## Topology 1: All-in-one host

CM, both backends (`contextmatrix-agent serve`, `contextmatrix-chat serve`),
and Docker on one machine. One host holds the credential and everything talks
over loopback.

**CM config (GitHub App):**

```yaml
github:
  auth_mode: "app"
  app:
    app_id: 123456
    installation_id: 78910
    private_key_path: /etc/contextmatrix/github-app/private-key.pem
```

The backends' `serve.yaml` files set only connectivity and their HMAC
`api_key`, with no `github:` block. `contextmatrix_url` points at CM's
loopback address and `container_contextmatrix_url` at the Docker bridge
gateway so containers can reach CM's MCP endpoint.

To use a PAT instead, swap the `github:` block for the PAT form
(`auth_mode: pat`, token via `CONTEXTMATRIX_GITHUB_PAT_TOKEN`). The backends
are unaffected.

## Topology 2: CM host + separate worker VM

CM on one host; both backends and Docker on a worker VM. The common production
shape when execution should be isolated from the coordination layer.

- **CM host:** holds the App private key or PAT. Configure `github:` exactly
  as in Topology 1.
- **Worker VM:** runs both backends with only their HMAC `api_key`,
  `contextmatrix_url` (CM as the VM sees it), and
  `container_contextmatrix_url` (CM as containers see it). No GitHub
  credential is mounted on the VM.

Workers obtain project-repo access from CM at run time: the agent backend
refreshes the running card's token from `GET /api/agent/git-credentials` into
`/run/cm-secrets`; chat workers fetch a per-repo token with their per-session
bearer from `GET /api/worker/git-credentials`. If the VM cannot reach CM,
workers cannot clone.

## Topology 3: CM in Kubernetes + worker VM

CM runs in Kubernetes with the App private key mounted as a Secret and
`ops.db`, `auth.db`, `images.db`, and the boards repo on a PVC; the backends
run on a worker VM as in Topology 2. See
[deployment-example.md](deployment-example.md) for the full manifests.

- **CM (k8s):** `CONTEXTMATRIX_GITHUB_AUTH_MODE=app` with the App ID,
  installation ID, and a `private-key.pem` mounted from a Secret.

  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: contextmatrix-github
  type: Opaque
  stringData:
    app-id: "123456"
    installation-id: "78910"
    private-key.pem: |
      -----BEGIN RSA PRIVATE KEY-----
      ...
      -----END RSA PRIVATE KEY-----
  ```

- **Worker VM:** identical to Topology 2.

## Choosing App vs PAT

| Question                                          | Answer pushes you toward                   |
| ------------------------------------------------- | ------------------------------------------ |
| Are you on a tenant with GitHub App restrictions? | PAT                                        |
| Should the token on the worker VM be short-lived? | App (installation tokens expire within 1h) |
| Do you want the simplest possible config?         | Either: both are one `github:` block on CM |

In doubt, start with a GitHub App: its credentials do not expire (only the
installation tokens minted from them do), so there is no rotation to forget,
and workers never hold a long-lived credential.
