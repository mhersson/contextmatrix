# Recommended GitHub auth topologies

The GitHub credential lives on **ContextMatrix only**, regardless of how you lay
out the deployment. CM uses it for boards sync, task-skills pull, issue import,
and branch listing, and it mints the short-lived per-run tokens that worker
containers use to clone and push project repos. The agent and chat backends
carry **no GitHub credential of their own** - they receive minted tokens from CM
at run time.

So "topology" here is about **where CM runs** and how the execution backends
reach it, not about distributing GitHub identities. This document covers three
layouts. For step-by-step App / PAT creation, see
[github-auth-setup.md](github-auth-setup.md).

**Which component touches which repo:**

| Repo                   | ContextMatrix                                   | Backends / worker containers                          |
| ---------------------- | ----------------------------------------------- | ----------------------------------------------------- |
| Boards repo            | clone / pull / push                             | not accessed                                          |
| Task-skills repo       | derive `{git_remote_url, ref}` pointer + mint token | backend clones the pointer using CM's minted token |
| Project repos (GitHub) | issue import (REST), branch list (REST)         | worker clones / pushes / opens PRs with a per-run token CM mints |

## Topology 1: All-in-one host

CM and both backends (`contextmatrix-agent serve`, `contextmatrix-chat serve`)
plus Docker on a single machine. Simplest surface - one host holds the GitHub
credential and everything talks over loopback.

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
`api_key` - no `github:` block. Because CM is on the same host, the backends
point `contextmatrix_url` at CM's loopback address and
`container_contextmatrix_url` at the Docker bridge gateway so containers can
reach CM's MCP endpoint.

Use a PAT instead of an App by swapping the CM `github:` block for the PAT form
(`auth_mode: pat`, token via `CONTEXTMATRIX_GITHUB_PAT_TOKEN`). Nothing else
changes - the backends are unaffected either way.

## Topology 2: CM host + separate worker VM

CM on one host; the two backends and Docker on a separate worker VM. This is the
common production shape when you want execution isolated from the coordination
layer.

- **CM host:** holds the GitHub App private key (or PAT) - the only place the
  credential exists. Configure `github:` exactly as in Topology 1.
- **Worker VM:** runs `contextmatrix-agent serve` and `contextmatrix-chat serve`
  with only their HMAC `api_key`, `contextmatrix_url` (CM as the VM sees it), and
  `container_contextmatrix_url` (CM as containers see it). No GitHub credential
  is mounted on the VM.

Worker containers on the VM obtain project-repo access from CM at run time: the
agent backend refreshes a per-run token into `/run/cm-secrets`; chat workers
fetch a per-repo token with their per-session bearer. If the VM cannot reach CM,
workers cannot clone - the credential path is CM, not the VM.

## Topology 3: CM in Kubernetes + worker VM

CM runs in Kubernetes (App private key mounted as a Secret; `ops.db`, `auth.db`,
and the boards repo on a PVC); the backends run on a worker VM as in Topology 2.
See [deployment-example.md](deployment-example.md) for the full manifests.

- **CM (k8s):** `CONTEXTMATRIX_GITHUB_AUTH_MODE=app` with the App ID,
  installation ID, and a `private-key.pem` mounted from a Secret. This is the
  sole holder of the GitHub credential.

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

- **Worker VM:** identical to Topology 2 - backends with HMAC keys and CM
  connectivity, no GitHub credential.

## Choosing App vs PAT

| Question                                                   | Answer pushes you toward |
| ---------------------------------------------------------- | ------------------------ |
| Are you on a tenant with GitHub App restrictions?          | PAT                      |
| Do you want short-lived tokens for blast-radius reduction? | App (1h installation tokens) |
| Do you want the simplest possible config?                  | App or PAT - both are one `github:` block on CM |

In doubt, start with a GitHub App: its credentials don't expire (only the
installation tokens minted from them do), so there's no annual rotation to
forget, and the per-run worker tokens inherit the same short lifetime.
