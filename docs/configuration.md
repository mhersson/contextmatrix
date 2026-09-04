# Configuration

How ContextMatrix finds its config file, which fields it needs to start, how
`CONTEXTMATRIX_*` environment variables override them, and where the server
keeps its state on disk. `config.yaml.example` in the repo root is the
fully-commented reference for every field; this page covers the rules around it.

## Install the config directory

The config directory is `$XDG_CONFIG_HOME/contextmatrix` when
`XDG_CONFIG_HOME` is set, otherwise `~/.config/contextmatrix`. Copy
`config.yaml.example` there as `config.yaml` and `workflow-skills/` next to it,
or let [contextmatrix-setup](https://github.com/mhersson/contextmatrix-setup)
manage the whole stack. Workflow skills default to `<config-dir>/workflow-skills/`
(`workflow_skills_dir` overrides).

## `contextmatrix config`

| Command                              | Effect                                                                                                                        |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| `contextmatrix config defaults`      | Print the complete default configuration as YAML: every key the loader accepts. Backends are present and disabled; host-dependent paths are empty. Reads nothing. |
| `contextmatrix config validate FILE` | Load `FILE` exactly as the server would (env overrides included). Exit 0 when the server would start, else print the first error and exit 1. A missing `FILE` is an error here, unlike `-config`, which falls back to defaults plus env overrides. |

`contextmatrix-setup` uses both: `defaults` is its schema, so it adds keys you
are missing and removes keys that no longer exist. Renaming a key upstream
therefore drops the user's old value at the next update; prefer adding the new
key and reading the old one for a release when a rename is unavoidable.

Two keys are exceptions to that merge rule. `backends.agent.enabled` and
`backends.chat.enabled` print as `false` so the backend key sets are visible,
but an omitted `enabled` means the backend is enabled, so a merge must never
add `enabled: false` on the schema's word alone. And `boards` prints only the
single mapping form, while the loader also accepts a list of named repos, so a
user file holding a list must have each entry merged against the mapping form
rather than the whole list replaced by it.

## Config file discovery

| Order | Source                                        | Rule                                                                                   |
| ----- | --------------------------------------------- | -------------------------------------------------------------------------------------- |
| 1     | `-config PATH`                                | Used when given. A missing file at PATH is not an error: the server runs on defaults plus env overrides |
| 2     | `$XDG_CONFIG_HOME/contextmatrix/config.yaml`  | Checked only when `XDG_CONFIG_HOME` is set                                             |
| 3     | `~/.config/contextmatrix/config.yaml`         | Checked when `XDG_CONFIG_HOME` is unset                                                |

With no flag and no file in the XDG location the server logs
`no config file found; use -config to specify a path` and exits 1.

YAML decoding is strict: an unknown key fails startup with a `parse config`
error naming the field.

Containers use rule 1 with a path that does not exist and configure
everything through environment variables (see
[Deployment example](deployment-example.md)).

## Minimal config

```yaml
port: 8080
mcp_api_key: "" # Bearer token for /mcp; set it for anything beyond localhost

boards:
  dir: ~/contextmatrix-boards # created and git-initialised on first start

github: # required: auth_mode must be "app" or "pat"
  auth_mode: pat
  pat:
    token: github_pat_... # or CONTEXTMATRIX_GITHUB_PAT_TOKEN
```

`github.auth_mode` is validated at startup even when nothing talks to GitHub:
`app` needs `app_id`, `installation_id` and `private_key_path`; `pat` needs a
non-empty `token`. The token is only used when a git remote, issue import,
branch listing, or a worker run needs it. See
[GitHub auth setup](github-auth-setup.md).

`auth.mode` defaults to `multi` (login required). Set `auth.mode: none` for
a zero-login laptop install. See [Authentication](authentication.md).

## Environment overrides

Every scalar field has a `CONTEXTMATRIX_*` override; env wins over the file,
which wins over built-in defaults. The name is the YAML path upper-cased
with `.` replaced by `_`:

| YAML path                               | Env var                                             |
| --------------------------------------- | --------------------------------------------------- |
| `port`                                  | `CONTEXTMATRIX_PORT`                                |
| `boards.git_auto_push`                  | `CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH`                |
| `auth.session_idle_ttl`                 | `CONTEXTMATRIX_AUTH_SESSION_IDLE_TTL`               |
| `github.issue_importing.sync_interval`  | `CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_SYNC_INTERVAL` |
| `instance.id`                           | `CONTEXTMATRIX_INSTANCE_ID`                         |

Exceptions to the rule:

| YAML path                        | Env var                                    | Note                                      |
| -------------------------------- | ------------------------------------------ | ----------------------------------------- |
| `backends.<name>.<field>`        | `CONTEXTMATRIX_BACKEND_<NAME>_<FIELD>`     | Singular `BACKEND`; NAME is `AGENT` or `CHAT` |
| `github.app.installation_id`     | `CONTEXTMATRIX_GITHUB_INSTALLATION_ID`     | No `APP_` segment                         |
| `github.app.private_key_path`    | `CONTEXTMATRIX_GITHUB_PRIVATE_KEY_PATH`    | No `APP_` segment                         |
| `backends.agent.model_allowlist` | `CONTEXTMATRIX_BACKEND_AGENT_MODEL_ALLOWLIST` | Comma-separated list                   |

Booleans accept `true`, `false`, `1`, `0`. A malformed boolean, integer, or
chat duration is logged and ignored, keeping the file value; other malformed
values fail validation at startup.

No env override exists for `stalled_check_interval`, `token_costs`,
`chat.resume_budget_tokens`, `chat.rehydration_timeout`, `mob.guests`,
`backends.agent.favorites`, `aa_model_map`, `model_priors`, or the list form
of `boards`. `CONTEXTMATRIX_BOARDS_*` applies to the single-repo form only;
setting one while `boards` is a list is a startup error.

## Command line

| Command                                                       | Purpose                                                          |
| ------------------------------------------------------------- | ---------------------------------------------------------------- |
| `contextmatrix`                                               | Run the server; config discovered as above                       |
| `contextmatrix -config PATH`                                  | Run with an explicit config file                                 |
| `contextmatrix auth reset-admin [--config PATH] <username>`   | Print a one-time password-reset link for a locked-out admin      |
| `contextmatrix auth rotate-master-key [--config PATH]`        | Re-encrypt the credential pool under a fresh master key          |

The `auth` subcommands run on the host against `auth.db`, require
`auth.mode: multi`, and take flags before the username. Details in
[Authentication](authentication.md#operator-escape-hatches). There is no
version flag; the build version is served in `GET /api/app/config`.

## Directories and stores

`<state>` below is `$XDG_STATE_HOME/contextmatrix`, or
`~/.local/state/contextmatrix` when `XDG_STATE_HOME` is unset. Tilde
expansion applies to every path field.

| Path                    | Config key / env                                                | Default                          | Holds                                                                      |
| ----------------------- | --------------------------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------- |
| Boards repository       | `boards.dir` / `CONTEXTMATRIX_BOARDS_DIR`                       | required                         | Cards and `.board.yaml` files. Created and `git init`ed when missing; cloned when `git_clone_on_empty` and `git_remote_url` are set |
| Workflow skills         | `workflow_skills_dir` / `CONTEXTMATRIX_WORKFLOW_SKILLS_DIR`     | `<config-dir>/workflow-skills`   | Lifecycle skills the MCP server hands to agents as prompts                 |
| Task skills             | `task_skills.dir` / `CONTEXTMATRIX_TASK_SKILLS_DIR`             | `<config-dir>/task-skills`       | Operator skills repo mounted into worker containers; git-initialised when missing |
| Auth store              | `auth.db_path` / `CONTEXTMATRIX_AUTH_DB_PATH`                   | `<state>/auth.db`                | Users, sessions, one-time tokens, credential pool (`multi` mode only)      |
| Master key              | `auth.master_key_file` / `CONTEXTMATRIX_AUTH_MASTER_KEY_FILE`   | `<state>/master.key`             | Hex 32-byte key encrypting pool secrets; auto-generated 0600 with a warning |
| Image store             | `images.db_path` / `CONTEXTMATRIX_IMAGES_DB_PATH`               | `<state>/images.db`              | Pasted images for private boards repos and project-less uploads            |
| Operational store       | `op_store.db_path` / `CONTEXTMATRIX_OP_STORE_DB_PATH`           | `<state>/ops.db`                 | Chat sessions and transcripts, model blacklist, Best-of-N outcomes, chat cost archive |
| Instance id             | `instance.id` / `CONTEXTMATRIX_INSTANCE_ID`                     | `<state>/instance_id`            | Generated `<hostname>-<6 hex>`; identifies this server on a shared boards repo |

`<config-dir>` means the directory holding the loaded config file, so
`-config /etc/contextmatrix/config.yaml` puts the skill defaults under
`/etc/contextmatrix/`. Workflow skills and task skills are two different
systems; see [Agent workflow](agent-workflow.md).

## Token cost rates

`token_costs` maps a model slug to USD-per-token rates (`prompt`,
`completion`, optional `cache_read` and `cache_write`) used for card cost
reporting. When `llm_endpoint.type` is `openai`, rates are filled from the
endpoint catalog and hand-listed entries act as overrides. Missing cache
rates derive as prompt x 0.10 (read) and prompt x 1.25 (write); a 0 means
unset, so a free cache rate cannot be expressed. Config file only.

## Troubleshooting

- **`no config file found; use -config to specify a path`** - copy
  `config.yaml.example` into the config directory, or pass `-config`.
- **`github.auth_mode is required`** or
  **`github.app.app_id is required when github.auth_mode is "app"`** - the
  installed template ships an `app` block with zero ids. Fill it in, or switch
  to `auth_mode: pat` with a token.
- **`boards.dir is required`** - set it in the file or via
  `CONTEXTMATRIX_BOARDS_DIR`.
- **Boards directory is not a git repository** - not an error: the server
  initialises one. A `shared: true` entry whose clone has no `origin` refuses
  to start; clone it with `git_clone_on_empty` or add the remote by hand.
- **MCP connection refused or 401** - check the server is running and the
  port matches. When `mcp_api_key` is set, every request needs
  `Authorization: Bearer <key>`; all failures return the same
  `{"error":"unauthorized"}`. See [MCP](mcp.md).
- **`pattern all:dist: no matching files found`** - the Go embed needs
  `web/dist`. Run `make test` (stubs it) or `make build` (builds it).
- **`make build`, `make test-frontend` or `make lint-frontend` fail with a
  missing `vite` or `eslint`** - run `make install-frontend` to populate
  `web/node_modules`.
- **Leftover `backends.runner` entry** - the runner backend is retired; the
  entry (or any `CONTEXTMATRIX_BACKEND_RUNNER_*` variable) fails startup.

## See also

- [Authentication](authentication.md) - `auth.mode`, bootstrap, credential pool
- [GitHub auth setup](github-auth-setup.md) - the `github` block end to end
- [GitHub issue import](github-issue-import.md) - `github.issue_importing.*`
- [Shared boards](shared-boards.md) - the `boards` list form and `shared: true`
- [Remote execution](remote-execution.md) - the `backends` block
- [Deployment example](deployment-example.md) - env-only container config
- [Architecture](architecture.md#file-layout) - on-disk layout of a boards repo
