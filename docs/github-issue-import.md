# GitHub issue import

ContextMatrix polls open GitHub issues for projects that opt in and creates a
card per issue. Import is one-way: cards are created once and never updated,
closed, or written back. GitHub authentication is a prerequisite; see
[GitHub auth setup](github-auth-setup.md).

## Enable it

Global switch in `config.yaml`:

```yaml
github:
  auth_mode: app # or pat; see github-auth-setup.md
  issue_importing:
    enabled: true
    sync_interval: "5m"
```

| Key                                  | Env                                                  | Default | Rule                                    |
| ------------------------------------ | ---------------------------------------------------- | ------- | --------------------------------------- |
| `github.issue_importing.enabled`     | `CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED`       | `false` | Starts the syncer at boot               |
| `github.issue_importing.sync_interval` | `CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_SYNC_INTERVAL` | `5m`    | Go duration; below `5m` fails startup   |

Per-project opt-in in the project's `.board.yaml`:

```yaml
repo: git@github.com:acme/widgets.git
github:
  import_issues: true
  card_type: task          # optional, default task
  default_priority: medium # optional, default medium
  labels: [triaged, agent] # optional; only issues carrying ALL of these
  # owner: acme            # optional override, see below
  # repo: widgets
```

| Key                | Default  | Meaning                                                        |
| ------------------ | -------- | -------------------------------------------------------------- |
| `import_issues`    | `false`  | Include this project in every sync cycle                       |
| `card_type`        | `task`   | Type given to imported cards; must be one of the board's types |
| `default_priority` | `medium` | Priority given to imported cards                               |
| `labels`           | none     | GitHub label filter; an issue must carry every listed label    |
| `owner`, `repo`    | derived  | Explicit GitHub owner and repository name                      |

## Owner and repo resolution

With no `owner`/`repo` override the syncer parses the project's `repo` URL:

| Form                                | Example                                   |
| ----------------------------------- | ----------------------------------------- |
| SSH scp-style                       | `git@github.com:acme/widgets.git`         |
| HTTPS, with or without `.git`       | `https://github.com/acme/widgets`         |
| SSH URL                             | `ssh://github.com/acme/widgets.git`       |

The host must be `github.com` or the configured `github.host`; any other
host makes the project silently ineligible (logged at debug level). The
override is honoured only when the project's `repo` URL also passes that host
check, so a `.board.yaml` typo cannot point the credential at a foreign host.
A project with no resolvable owner/repo is skipped.

## Sync cycle

The syncer runs once at startup, then every `sync_interval`. For each project
with `import_issues: true` it:

1. Resolves the GitHub client: the project's bound `github_credential` in
   `multi` mode, otherwise the instance-wide `github.*` credential. A binding
   that fails to resolve skips the project for that cycle.
2. Fetches open issues (pull requests are dropped) filtered by `labels`.
3. Builds the external id `owner/repo#<number>` and skips the issue when a
   card with that id already exists in the project.
4. Creates a card in the board's first state with the issue title as title,
   the issue body as body, the issue labels as card labels, `card_type`,
   `default_priority`, and a `source` block.

Rate limiting and permission errors are logged and the project is skipped
for that cycle; other projects keep syncing. A panic in one cycle is
recovered and does not stop the ticker.

Imported cards start with `vetted: false`. A non-human agent cannot claim an
unvetted card that has a `source`; a human ticks **Content vetted** in the
card panel after reading the issue. This is the guard against untrusted
issue text driving an agent run. See [Running cards](running-cards.md).

## The `source` field

```yaml
source:
  system: github
  external_id: acme/widgets#42
  external_url: https://github.com/acme/widgets/issues/42
```

`external_id` is the dedup key and is queryable through
`GET /api/projects/{project}/cards?external_id=...`. `external_url` must be
`http` or `https`. The field is set by the importer and by
`POST /api/projects/{project}/cards`; MCP `create_card` cannot set it.

## What the UI shows

- **Board and chip row** - a GitHub icon next to the type chip, and an
  `unvetted` chip until a human vets the card.
- **Card panel** - a header chip `#owner/repo#N` linking to the issue; the
  metadata section repeats the link next to the **Content vetted** checkbox.
- **Toast** - `New issue from GitHub: <title>` when a card with
  `source_system: github` arrives over the board's event stream.

## GitHub Enterprise

Set `github.host` (for example `acme.ghe.com`). The API base becomes
`https://api.<host>` unless `github.api_base_url` overrides it, and project
`repo` URLs on either `github.com` or the enterprise host are accepted. Pool
credentials carry their own `host` and `api_base_url`, so a project bound to
an enterprise credential imports from that host. Details in
[GitHub auth setup](github-auth-setup.md#github-enterprise-ghec-dr--ghes).

## Limits

- Open issues only; closing an issue on GitHub does not move the card, and
  edits to the issue are not re-synced.
- One card per issue per project. Two projects pointing at the same
  repository each get their own card.
- Nothing is written back to GitHub.

## See also

- [GitHub auth setup](github-auth-setup.md) - App vs PAT, enterprise hosts
- [Authentication](authentication.md#github-credential-pool-and-per-project-binding) -
  per-project credential binding
- [Boards](boards.md) - the full `.board.yaml` reference
- [Data model](data-model.md) - card fields including `source` and `vetted`
- [Configuration](configuration.md) - env override rules
