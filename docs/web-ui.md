# Web UI

The React frontend embedded in the ContextMatrix binary: the kanban board, the
dashboards, the global chat surface and the admin pages. This page is a feature
reference for people using the UI; execution semantics live in
[running cards](running-cards.md).

## Routes

| Route                                       | View                                                     |
| ------------------------------------------- | -------------------------------------------------------- |
| `/`, `/all`                                 | All-projects dashboard                                   |
| `/projects/:project`                        | Board. `?card=ID` opens that card's detail panel         |
| `/projects/:project/settings`               | Project settings                                         |
| `/chat`, `/chat/:id`                        | Global chat surface. `/chat?new=1` opens the New Chat dialog |
| `/playbooks`, `/playbooks/:id`              | Playbook list and detail (see [playbooks](playbooks.md)) |
| `/admin/users`, `/admin/credentials`        | Admin pages, `multi` auth mode only                      |
| `/admin/chats`, `/admin/model-selection`    | Admin pages, `multi` auth mode only                      |

Every view updates live from one Server-Sent Events stream per tab
(`GET /api/events`); no view polls for card changes.

## Board view

- **Columns** are the project's `states` in `.board.yaml` order. Drag a card
  between columns to transition it; a drop the board's `transitions` forbid is
  ignored. Mouse drags start after 5 px, touch drags after a 250 ms hold.
- **Card detail panel** opens on click. Tabs: Chat (only while a session is
  live), Automation, Info, Danger. `Escape` closes, `Ctrl`/`Cmd`+`S` saves.
- **Collapsing**: columns collapse to a strip, cards to a header row. Both sets
  are stored per project in `localStorage` under
  `contextmatrix-collapsed-columns-<project>` and
  `contextmatrix-collapsed-cards-<project>`. The board header chrome collapses
  too and starts collapsed on viewports under 800 px tall.
- **Filter bar**: free-text search over id, title, labels, assignee, agent and
  branch name, plus chips Mine, Assigned to me (`multi` mode), Critical, High,
  Bugs, Autonomous and worker:running. A parent stays visible when one of its
  subtasks matches. `Escape` clears search and chips.

## Column sorting

Each column header has a sort menu. The choice is stored per project under
`contextmatrix-column-sort-<project>`; every column keeps its own.

| Mode          | Order                                                              |
| ------------- | ------------------------------------------------------------------ |
| Recent        | Most recently updated first (default)                              |
| ID up / down  | Card id ascending or descending                                    |
| Priority      | Last entry of the board's `priorities` list first, then created   |
| Type          | bug, feature, task, subtask, then board types in `types` order     |
| Manual        | Hand-made order, stored under `contextmatrix-manual-order-<project>` |

Drag-to-reorder rules:

- Dropping a card on another card in the same column moves it there and
  switches that column to Manual. Dropping on the column body or header in the
  same column does nothing.
- Dropping into another column is a state transition. It never flips the target
  column to Manual; a target already on Manual records the drop position.
- Cards missing from a Manual order (new arrivals, cards returning from a
  terminal state) sort to the bottom, most recently updated first. Choosing
  Manual with nothing stored captures the current visual order.
- The order is per browser. Nothing is shared or synced.

## Dashboard

- **Per project**: the board header carries a metrics ribbon (Active agents,
  In flight, Stalled, Shipped today, Shipped 7d with sparklines) and a rail of
  the agents currently working, fed by `GET /api/projects/{project}/dashboard`.
- **All projects** (`/all`): KPI tiles (Open tasks, In progress, Done today,
  Cost 30d, Chat cost 30d), agents on duty, cost by model for the last 30 days
  (a card is attributed to the model it used last), a projects table, an
  activity feed and a top-cards panel. With more than one boards repository the
  projects table and the sidebar group projects by repository.

## Execution console

Shown only when a task backend is configured. The Console button in the board
header (keyboard `c`) opens a pane below the board that streams live container
logs from `GET /api/worker/logs?project=...` over SSE. A drag divider resizes
the split, a dropdown filters by card, and a Stop All button appears while
workers are active.

## Running cards from the UI

The card panel header shows one primary action: Stop while the worker is
queued or running, otherwise a curated transition (Mark done, Unblock, Resume,
Re-open) when the board allows it, otherwise Run Auto or Run HITL. Which run
button appears follows the Autonomous mode switch in the Automation tab, which
also holds Best of N and mob settings. What a run does is documented in
[running cards](running-cards.md).

The Chat tab is the run's live transcript: interactive for a HITL run, read-only
for an autonomous run and for a card another instance claimed on a shared
board. Promote-to-autonomous lives in that tab.

## Global chat

`/chat` hosts board-aware chat sessions that outlive any card.

- Up to 4 panes in a 2x2 layout. Opening a fifth chat replaces the pane focused
  least recently and shows a toast, "Closed X to make room for Y", with Undo
  for 6 seconds.
- Drag a chat from the sidebar onto a pane to place or swap it; a pane can also
  split to open an empty slot.
- Panes, focus and divider sizes persist under the `chat_layout` key; the
  focused chat id under `last_chat_id`. `/chat/:id` opens that chat in a pane.

## Image attachments

Paste, drag-and-drop or pick PNG, JPEG, WebP or GIF files into the card body
editor. The client refuses files over 10 MiB and more than 3 uploads at once.

- `POST /api/images` re-encodes and shrinks the image to fit 1024x768, and
  names it by the first 16 hex characters of the SHA-256 of the result, so
  identical uploads share one id. The editor inserts `![](url)` markdown.
- A project in a shared boards repository stores its images as
  `<project>/images/<id>.<ext>` files in that repository, one commit per
  upload, so every instance serves them. Everything else lives in `images.db`
  (default `$XDG_STATE_HOME/contextmatrix/images.db`).
- `get_card` and `get_task_context` attach the referenced images inline as
  base64, capped at 10 images and about 20 MiB per response.

## Appearance

The sidebar footer menu (the user chip in `multi` mode, an Appearance chip in
`none` mode) sets Light or Dark and the palette: Everforest, Radix or
Catppuccin.

| Setting     | Server default                     | Browser override (`localStorage`)   |
| ----------- | ---------------------------------- | ----------------------------------- |
| Light/Dark  | Light                              | `theme`                             |
| Palette     | `theme` key in `config.yaml`, default `everforest` | `palette`, wins once set |

## Projects

- **New Project** in the sidebar opens a wizard: boards repository (when
  several are configured), display name, project name (slug derived from the
  display name), card prefix (derived, uppercase) and code repo URL. It creates
  the board with the default states, types, priorities and transitions from
  [boards](boards.md).
- **Project Settings** (`s`) edits everything in `.board.yaml`: repos, states,
  types and priorities (`stalled` and `not_planned` cannot be removed), the
  transition matrix, remote-execution worker images, the verify gate, GitHub
  issue import, default skills and, in `multi` mode, the GitHub credential
  binding. Deleting the project sits in the Danger Zone. Both are admin-only in
  `multi` mode.

## Keyboard shortcuts

Shortcuts are ignored while typing in a field and when a modifier is held.

| Key            | Action                                            |
| -------------- | ------------------------------------------------- |
| `n`            | New card                                          |
| `b`            | Back to the board                                 |
| `s`            | Project settings                                  |
| `c`            | Toggle the execution console                      |
| `1` to `9`     | Switch to the nth project in the sidebar          |
| `Escape`       | Clear filters and search, or close the card panel |
| `Ctrl`/`Cmd`+`S` | Save the open card panel                        |

## Sync status

On a shared boards repository the board footer shows a `git sync` label with
the time of the last sync; hovering lists sync errors, unpushed commits and
the merge resolutions the syncer made, and clicking triggers a sync. The
sidebar shows a sync dot per repository and the all-projects dashboard a sync
line per repository. Details in [shared boards](shared-boards.md).

## See also

- [Boards](boards.md) - creating a board and the workflow contract
- [Playbooks](playbooks.md)
- [Running cards](running-cards.md)
- [Configuration](configuration.md) - `theme`, `boards`, `images`
- [API reference](api-reference.md) - the endpoints the UI calls
