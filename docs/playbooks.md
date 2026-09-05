# Playbooks

A playbook is an ordered, cross-project runbook: card references whose status
is read live from the board, mixed with manual gate steps a human checks off.
Playbooks coordinate order for people and planning sessions; ContextMatrix
never executes one.

## Entries

| Kind     | Fields                                  | Complete when                              |
| -------- | --------------------------------------- | ------------------------------------------ |
| `card`   | `project`, `card`, optional `note`      | The card is `done` or `not_planned`        |
| `manual` | `text`, optional `note`, `done` stamps  | A human checked it off                     |

- A card entry must reference an existing card when added. Its title, state
  and assigned agent are resolved on every read and never stored. A card or
  project deleted later leaves the entry in place, flagged `missing`, counting
  in the total but never as complete.
- The same `{project, card}` pair cannot appear twice in one playbook.
- Checking a manual entry stamps `done_by` and `done_at` from the caller and
  the server clock; unchecking clears both.
- `note` is a human-only channel on both kinds. It is editable from the UI and
  the MCP tools but never reaches an agent's task context, and no workflow
  skill reads playbooks at all.
- Progress is `complete/total`, derived on read. An empty playbook is valid.

## Detail view

`/playbooks` lists playbooks still in progress as rows, each with a miniature
of the detail rail: one node per entry (manual steps as gates), solid green
where a step is complete, an aqua pulse on an `in_progress` card, a purple
ring on the first incomplete entry, a dashed red node for a broken reference.
Longer playbooks (over 20 entries) fall back to the segmented meter. Each row
also names its next entry. Completed playbooks sit under a "Completed" rule as
one-line receipts, open by default. "New playbook" opens a ghost row at the
top of the list: name it, optionally describe it, pick the boards repo on a
multi-repo instance, and Enter creates. The sidebar link jumps straight to the
detail page when exactly one playbook exists.

`/playbooks/:id` shows the entries as a route: a rail threads through the
nodes, solid where the previous step is complete and dashed where it is not.
Card nodes carry the sequence number and a state chip; an `in_progress` card
pulses as "agent active"; broken references get a dashed red border. The first
incomplete entry is spotlighted and repeated in the side panel as "Next up".
Drag an entry to reorder; the composer in the side panel appends a card (picked
from any project) or a manual step to the end. Notes edit inline. The page
updates live over SSE.

## Storage

- One file per playbook at `playbooks/<id>.yaml` in the boards repository,
  outside every project directory. Order is array order.
- `id` is a slug derived from the title at creation and never changes;
  collisions get a numeric suffix. Entry ids are `e<N>` from a persisted
  counter and are never reused.
- With several boards repositories each has its own `playbooks/` directory.
  `boards_repo` on create picks one (default: the first configured); ids are
  unique across all of them and every response carries `boards_repo`.
- `playbooks` is a reserved project name. A pre-existing project by that name
  disables the playbook subsystem at startup, with a logged error, until it is
  renamed.

Parsing is lenient: unknown fields are ignored and a file that fails to parse
is skipped with a warning.

## API and MCP

Playbook tools and routes are registered only while the subsystem is enabled.
All eight MCP tools are ungated; the human-only nature of notes is a contract
the agent-facing context honours, not a permission check. `agent_id` is
required on mutations for `created_by` and `done_by` attribution.

| MCP tool                | REST                                              | Does                                                          |
| ----------------------- | ------------------------------------------------- | ------------------------------------------------------------- |
| `list_playbooks`        | `GET /api/playbooks`                              | Summaries: progress, segments, project count, gates, next entry |
| `get_playbook`          | `GET /api/playbooks/{id}`                         | Full detail with every entry resolved against the card store  |
| `create_playbook`       | `POST /api/playbooks`                             | `title`, `description`, `boards_repo`, initial `entries`; all-or-nothing |
| `update_playbook`       | `PATCH /api/playbooks/{id}`                       | Title and description; the id never changes                   |
| `delete_playbook`       | `DELETE /api/playbooks/{id}`                      | Removes the file; referenced cards are untouched              |
| `add_playbook_entry`    | `POST /api/playbooks/{id}/entries`                | Appends one card or manual entry                              |
| `update_playbook_entry` | `PATCH /api/playbooks/{id}/entries/{entryId}`     | `done` and `text` (manual only), `note` (both), `position`    |
| `remove_playbook_entry` | `DELETE /api/playbooks/{id}/entries/{entryId}`    | Removes one entry                                             |

`position` is the entry's final index after the move; values past the end
clamp, negative values are rejected. Setting `done` or `text` on a card entry
fails with a validation error. Request and response bodies are in the
[API reference](api-reference.md#playbook-endpoints).

## YAML shape

```yaml
id: alpha-rollout
title: Alpha feature rollout
description: Ship alpha behind the flag, then flip it
created_by: human:alice
created_at: 2026-08-20T09:00:00Z
updated_at: 2026-08-20T10:30:00Z
next_entry_id: 4
entries:
  - id: e1
    type: card
    project: project-alpha
    card: ALPHA-101
    note: merge this one first
  - id: e2
    type: manual
    text: Rebuild the worker image and redeploy
    done: true
    done_by: human:alice
    done_at: 2026-08-20T10:30:00Z
  - id: e3
    type: card
    project: project-beta
    card: BETA-042
```

## See also

- [Data model](data-model.md#playbooks) - rules and Go types
- [API reference](api-reference.md#playbook-endpoints) - wire shapes
- [Boards](boards.md) - the boards repository layout
- [MCP](mcp.md) - the full tool catalogue
