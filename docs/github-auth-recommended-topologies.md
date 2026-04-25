# Recommended GitHub auth topologies

ContextMatrix has two binaries (the server and the runner). Each has
its own GitHub auth identity, configured independently. This document
covers three deployment patterns and provides side-by-side configs.

For step-by-step App / PAT creation, see
[github-auth-setup.md](github-auth-setup.md).

## Topology 1: Single App, both binaries *(recommended default)*

Use this when a single team owns both the server and the runner and
you want the simplest auth surface.

**Setup:**
1. Create one GitHub App (e.g., `contextmatrix-yourorg`).
2. Install it on: boards repo, task-skills repo, every project repo CM
   tracks.
3. Give both binaries the same App ID, installation ID, and private
   key.

**Server config:**
```yaml
github:
  auth_mode: "app"
  app:
    app_id: 123456
    installation_id: 78910
    private_key_path: /etc/contextmatrix/github-app/private-key.pem
```

**Runner config:**
```yaml
github:
  auth_mode: "app"
  app:
    app_id: 123456                    # same as server
    installation_id: 78910            # same as server
    private_key_path: /etc/contextmatrix-runner/github-app/private-key.pem
```

**k8s server Secret:**
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

**Runner systemd snippet:**
```ini
[Service]
Environment=CMR_GITHUB_AUTH_MODE=app
Environment=CMR_GITHUB_APP_ID=123456
Environment=CMR_GITHUB_INSTALLATION_ID=78910
Environment=CMR_GITHUB_PRIVATE_KEY_PATH=/etc/contextmatrix-runner/github-app/private-key.pem
```

## Topology 2: Single PAT, both binaries

Use this when GitHub App creation is restricted in your organization.

**Setup:**
1. Create one fine-grained PAT under a service account.
2. Grant access to: boards repo, task-skills repo, every project repo.
3. Distribute the same token to both binaries via env vars.

**Server config:**
```yaml
github:
  auth_mode: "pat"
  pat:
    token: ""    # supplied via CONTEXTMATRIX_GITHUB_PAT_TOKEN env var
```

**Runner config:**
```yaml
github:
  auth_mode: "pat"
  pat:
    token: ""    # supplied via CMR_GITHUB_PAT_TOKEN env var
```

**k8s server Secret:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: contextmatrix-github
type: Opaque
stringData:
  pat: github_pat_xxxxxxxxxxxxxxxxxxxxx
```

**Server env (referencing Secret):**
```yaml
- name: CONTEXTMATRIX_GITHUB_PAT_TOKEN
  valueFrom:
    secretKeyRef:
      name: contextmatrix-github
      key: pat
```

**Runner systemd snippet:**
```ini
[Service]
Environment=CMR_GITHUB_AUTH_MODE=pat
EnvironmentFile=/etc/contextmatrix-runner/github-pat.env
# (file contains CMR_GITHUB_PAT_TOKEN=github_pat_xxx)
```

## Topology 3: Mixed — App on server, PAT on runner

Use this when the runner runs on infrastructure where mounting an App
private key is awkward (e.g., a multi-tenant build host), or when you
want a per-binary audit trail.

**Setup:**
1. Create a GitHub App. Install it on: boards repo, task-skills repo,
   every project repo. (Server uses this.)
2. Create a fine-grained PAT. Grant access to: every project repo.
   (Runner uses this; runner doesn't touch the boards or task-skills
   repos.)

**Server config:** identical to Topology 1's server.

**Runner config:** identical to Topology 2's runner.

The token paths and Secret manifests are the union of the two
single-method topologies. Apply each to its respective binary.

## Choosing for production

| Question | Answer pushes you toward |
|---|---|
| Are you on a tenant with App restrictions? | Topology 2 (PAT) |
| Do you want short-lived tokens for blast-radius reduction? | Topology 1 (App) |
| Are server and runner managed by separate teams? | Topology 3 (mixed) |
| Do you want the simplest possible config? | Topology 1 (App) |

In doubt, start with Topology 1.
