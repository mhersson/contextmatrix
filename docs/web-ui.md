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
| `/admin/chats`                              | Chat administration (admin; open in `none` mode)         |
| `/admin/model-selection`                    | Tier ladders (admin; open in `none` mode)                |

Every view updates live from one Server-Sent Events stream per tab
(`GET /api/events`); no view polls for card changes.

## Board view

- **Columns** are the project's `states` in `.board.yaml` order. Drag a card
  between columns to transition it; a drop the board's `transitions` forbid is
  ignored. Mouse drags start after 5 px, touch drags after a 250 ms hold.
- **Card detail panel** opens on click. Tabs: Chat (only while a session is
  live), Automation, Info, Danger. `Escape` closes, `Ctrl`/`Cmd`+`S` saves.
- **Create card panel** (`n`) closes on `Escape`; once a title, body, label,
  parent, assignee, model pin, skill or mob guest is entered it asks before
  discarding.
- **Card bodies and chat messages** render as GitHub-flavoured markdown through
  an allowlist sanitizer. Allowlisted HTML renders (images over http, https
  or a relative path, `<details>`/`<summary>`, `<br>`, `<kbd>`,
  `<sub>`/`<sup>`, `<s>`/`<ins>`, definition lists, tables); scripts, styles,
  event handlers, forms and frames never become live. Other tags and comments
  show as literal text, except CM's own line-start `<!-- cm:meta ... -->`
  machine markers (and the legacy `<!-- cm:tier=... -->`), which the agent
  strips before a model reads the body. Attribute values the allowlist drops
  are not shown. Bodies arrive from agents and from GitHub and Jira imports.
- **Models used** (Info tab) answers what a card cost and where it went: the
  total including subtasks; a cost bar and legend split by role (review,
  execute, plan, judge, document, gates = pr_gates + integrate, plus a hatched
  subtasks share), shown
  when there is more than one share; this card's buckets grouped under role
  headers with the step words (seat, moderator, gate, ...) and, over two or
  more rows, the role subtotal;
  then one block per costed subtask with its card id (click to open) and
  model rows. Colour means role and nothing else. `*` marks amounts that
  include rate-table estimates. Roles come from the phase the agent reports
  on `report_usage`; buckets written before roles existed, or reported with
  no phase (chat-driven cards), show as `other`. That spend cannot be
  re-attributed, so there is no migration.
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
  The rail's Capacity section shows running worker containers against the
  backend cap from `GET /api/backend/health`, and reads "backend unreachable"
  whenever the last poll failed; the section is hidden when no task backend is
  configured.
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
[running cards](running-cards.md). A card in a runnable playbook has its five
playbook-owned settings locked on the Automation tab with a link to the
playbook; while the run is active it also shows a yellow queued-in-playbook
signal and a disabled run button.

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
- The New chat dialog shows two tiles, **Chat** and **Blueprint**, when an
  architect backend is configured (`architect_enabled` in app config); without
  one it creates chats only. Both kinds take an optional title. A chat picks one
  project from a list (or Across projects, with no checkout) and a model. A
  blueprint picks one or more target projects, one of four described modes
  (default, auto, auto + emit, yolo), a model (defaulting to the architect's
  default, kept separate from the chat pick) and a cost cap preset ($1, $2, $5,
  $10 or no cap), in that order, and runs on the architect backend instead of a
  chat worker. The footer says why Create is disabled, including a cost cap on a
  model CM cannot price; Ctrl+Enter anywhere, or Enter in the title, creates.
  The button names the outcome (Create chat, Create blueprint, or Create and run
  for yolo). A blueprint whose architect does not start keeps the dialog open
  with the error, and with unchanged settings, Create then retries the same
  session. The sidebar marks blueprint sessions with an aqua drafting-compass
  icon. The pane header shows a blueprint's mode and cost cap, and the
  architect's spend from the blueprint view (the figure the scale bar shows)
  where a chat's header shows CM's estimate from its token counts; and the admin
  Chats page lists every session's kind, mode and cap. The architect's questions
  and gates show reply buttons under the message. A question's options are
  stacked full-width buttons showing the whole option text. The question bubble
  is tinted, with a 5px left bar and accent-bordered options in pink in the dark
  themes and a dusty rose to maroon in the light ones, and the suggested option
  carries a "recommended" chip. A prompt marked as a gate keeps compact buttons
  in a row. A click sends that reply as your message, a feedback action such as
  Reject asks for text first, in a box labelled with the architect's question
  when it sends one (an approval over critical findings asks why), and typing
  the reply works too. A prompt closes when a reply answers it or a later
  prompt replaces it. It then shows a one-line summary instead of buttons, and
  a closed non-gate prompt keeps its options above the summary as a smaller
  list with the answering option marked.
  A closed question's list and summary render in the normal text colour on the
  tint; gates and neutral prompts keep the muted grey. The buttons are disabled
  while the session is not live or its stream is disconnected, and prompts show
  with the Text filter off.
- A question round from the architect docks at the bottom of the pane in place
  of the message box while it waits for you; the transcript keeps scrolling
  above it. It has one tab per question, ticked once it has an answer (stored by
  the architect or picked here) and dotted while open, an Assumptions tab when the stage states
  assumptions, and a Submit tab. A question tab shows the question, why it
  matters and numbered options; the suggested one carries a Recommended chip,
  and clicking an option selects it. An option's "pros · cons · N sources" link
  shows them (links and code paths; with the architect's research off, its code
  paths and "model knowledge" instead of links) and "hide" folds them; each
  option opens on its own. The last option on every question is "Type your
  own answer". The Assumptions tab accepts each assumption or turns it into a
  question. The Submit tab reviews every answer: "Submit all N" sends them as
  final, and "Send N now, answer the rest later" stores the ones picked so far
  on the architect, so they show on any device and survive leaving, and the
  round stays open. A sent reply shows no transcript bubble: the docked block
  and, once closed, the round's summary are its record; a reply whose round is
  not loaded shows as a plain message. Under a dotted line on every tab, "Chat about these
  questions" holds the thread about the round and is the pane's only text box
  while the round is open; the thread scrolls on its own, so the box and the
  foot stay in view. What you type there reaches the architect as a plain
  message, so session commands still work, and shows in the round's thread, not
  as a transcript bubble: right away as your line, until the architect
  republishes the round with its own thread. A message stored when no round
  waits any more shows as an ordinary bubble. Left and right arrows switch tabs,
  1-9 pick an option on a question tab and Enter moves to the next tab. Picks,
  assumption decisions and typed answers not yet sent are kept in this browser
  per round, so closing the pane, reloading or navigating away restores them;
  they count in "K of N answered" and the tab ticks, and a restored block opens
  on its first unanswered tab (picking never moves you on);
  one is dropped when the architect has since stored that question or decision,
  or revised the option it picked, and a round's drafts are cleared when it
  closes or a newer round replaces it. The architect republishes a round
  whenever it changes, and only the newest version shows. A closed round (every
  question answered and the stage resumed, or the pipeline went past it), and a
  round a newer one replaced, collapses into a card in the style of a sheet
  reference, titled after the asking stage's questions (or assumptions, for a
  round of only assumptions), with how many of each, the persona and what
  became of the round: answered or all accepted by you or the responder, how
  many were answered, accepted, turned into questions or not decided, or no
  longer open for a replaced round. Clicking the card, or Enter or Space on
  it, opens it in place: each question with its number, title and text and the
  stored answer beneath it, any question left unanswered marked "not answered",
  then each assumption with its decision (accepted, turned into a question or
  not decided). At the bottom of the open card a collapsed "Chat about these
  questions (N)" opens the round's thread of N messages, when it has one. A
  round the auto responder answered arrives closed and shows as the same card,
  marked answered by the responder, and the message box stays. While the session
  is not live or its stream reconnects the round stays docked with its answers
  disabled; its chat box sends whenever the message box would.
- A blueprint pane adds a scale bar, a sheet and marker rows to the chat pane:
  - The scale bar under the pane header draws one segment per pipeline step:
    done steps filled green, the current step hatched (aqua while a run works,
    the question accent while the blueprint waits for you), pending steps
    outlined, a failed step outlined red and folded slicing as a narrow segment;
    gates carry a notch. When the pipeline ended before the sign-off (a stage
    left nothing to build on, or the readiness rounds ran out), the step that
    stopped it is outlined red and opens the version that stopped it, every step
    it never reached is dashed and opens nothing, and the stopping step's newest
    version reads Stopped on the sheet unless a gate decided it. A blueprint cut
    into two or more slices draws each slice's steps together, bracketed under
    the bar with the slice's id; the slice in progress also shows its title, in
    the colour of its current segment. Beside the bar the current stage, with
    its slice when there are several (Solution design · SL2), says who is doing
    what; on an ended blueprint it names the stopping step and, in red, why the
    pipeline stopped. Hover a segment for its persona, status and cost; click a
    finished one to open its artifact. The right end shows the architect's spend
    against the blueprint cap; the meter turns orange at 80% and red at the cap.
    Click it (it is a button, also with no cap set) for a popover with the
    spend, the cap, +$2 / +$5 / +$10 raises (added to the cap, or to the spend
    with no cap), an amount field and a "No cap" choice. An amount at or below
    the spend is refused, as is any amount on a model CM cannot price (only "No
    cap" is offered, and only when a cap is set). Raising the cap continues a
    blueprint that stopped on its cap. The change goes to the architect as a
    `cap` command that the transcript does not show; the architect's
    confirmation line is the visible record, and the cell follows the cap the
    architect republishes; the pane header's cap follows after a successful
    save.
  - The sheet renders each artifact (need, slices, shared decisions, design,
    grounding, acceptance, cards, readiness, sign-off, board) as a document.
    Long lists are tables with the row count beside each heading: the need's
    outcomes, each with its id and the quality it states, and the outcomes the
    shared decisions or the design add; the need's assumptions and the design's
    (when it states any) with their basis, the slices with what each delivers,
    its projects and the slices it comes after, and their assumptions with a
    status; each slice's acceptance criteria with their project and the manual
    checks with their slice; the cards sheet's manual checks outside the
    playbook order with the checks they cover; and on the sign-off the run
    order, the assumptions (your answer, status, basis and source chips, and the
    stage that stated each) and the manual checks after the run. Below 520px of
    sheet width each table row stacks into a block; the sign-off's assumptions,
    with more columns, stack below 720px. A basis chips what it rests on:
    answers (Q1, round2.A1) blue, sources (S1) green, and stated assumptions
    (A3) and outcomes (O1) grey. Tabs follow the current stage until you pick
    one; a pick holds until the current stage, or the slice it runs for,
    changes. Stages without an artifact are dimmed and stages the pack does not
    run are hidden. The shared decisions sheet shows the decisions every slice
    builds on as the design sheet shows a slice's, with their contracts and
    assumptions. The design sheet heads its decisions with the projects they
    cover; a design that holds several slices, as designs made before per-slice
    design do, heads each slice's decisions with its id as well. A blueprint cut
    into two or more slices runs design, grounding, acceptance and cards once
    per slice: those four tabs sit between separators, and while one is open a
    row of slice tabs (each slice's id and title) opens under it, joined to the
    open tab by a shared shade. The chosen slice sits on the paper colour, a
    slice waiting for you shows in the question accent, and slices without that
    sheet yet are dimmed. Switching stage tabs keeps the chosen slice, and the
    sheet names the slice under its title. A blueprint with one slice shows none
    of this and no Shared tab. The title block names the sheet, its revision (a
    picker when there are several), who drew it, who checked it and its status.
    Checked by gives the reviewer and score, in red and ending "failed" when the
    shown version's own review failed (a score under the pass mark or a critical
    finding not fixed). Click it for the findings ledger: the round, the pass
    mark, the verdict and whether it went back for a revision, then each
    finding's id, a severity badge (critical, major or minor) and the reviewer's
    status (open, fixed in rev N or still open in rev N); a critical finding not
    yet fixed sits on a red wash. Beside it the builder's note shows as a claim,
    "rev N says: ...", or else the revision the finding was sent to; only the
    reviewer's status says a finding is fixed. An older architect's review shows
    no round or verdict and never reads failed; its findings show as a numbered
    list without id, severity or status. Stamps mark approved, rejected,
    superseded and folded versions. On the newest version a dashed cloud marks
    what changed, and Compare strikes removed words and highlights added ones.
    At a gate a bar above the title block repeats the gate's buttons. When the
    version at the gate has its own review, the bar also names each critical
    finding not yet fixed, with its id, status and text, and, when it has
    findings, a Findings (N) toggle opens the whole ledger there. While a
    question round waits, the asking stage's sheet shows the version it owes
    with the status Waiting on N questions (or the assumptions still undecided).
    While a run works the sheet shows its turns, tool calls and tokens, and
    fills in as the model writes when the model streams its draft.
  - From 760px pane width the sheet sits beside the transcript at 46% with a
    draggable divider (320px minimum each side). Narrower panes show an edge
    strip that opens the sheet over the transcript; on a phone a Sheet
    button in the pane header does. A new artifact version opens a closed
    sheet, except over the transcript while a question or a question round
    waits, when the strip or the button shows a dot. Under 560px the
    current-stage label moves into the bar's tooltip. Sheet width and open
    state are not stored.
  - The transcript shows a divider at each stage start and a sheet reference
    for each stored artifact; clicking a reference opens that version. While
    a blueprint has two or more slices, dividers, references, review cards
    and question round headers name the slice (Design · SL2).
  - Each reviewer verdict shows as a closed review card: the reviewer, the stage
    and version, its verdict with a (?) that explains the pass mark: sent back
    (orange) when the version went back for a revision, which a pass with an
    open major finding does too, passed (green) when it passed and went to the
    gate, and failed (red) when the stage's last round fell short and went to
    the gate anyway, the findings counted by status, and, when known, the round
    out of the stage's limit. Opened, it shows the reviewer's summary, then the
    findings under Still open, New this round and Fixed (folded until clicked),
    critical first, each with its severity and status. Long text clamps behind
    read more, and a finding's details link shows the reviewer's note and the
    builder's claim. A verdict whose review the view does not hold yet shows as
    its plain line.
  - The pane loads the architect's view, then follows it live on the
    session's chat stream, with no connection of its own. It loads the view
    again after the stream reconnects and whenever a stage divider, sheet
    reference, review or prompt reaches the transcript, so a lost live update
    heals.
  - When the view cannot be loaded and the stream has brought none, the sheet
    says the architect cannot be reached, with a Retry, and the scale bar
    hides; the transcript keeps working. An architect without the blueprint
    endpoints leaves the pane a plain transcript.

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

The sidebar footer menu (the user chip in `multi` mode, a Settings chip in
`none` mode, which also holds the admin pages that exist without accounts)
sets Light or Dark and the palette: Everforest, Catppuccin, GitHub or Ayu.

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
- **Project Settings** (`s`) edits everything in `.board.yaml`, grouped into
  five tabs under a header that carries the project's name, prefix and card
  count. **Source** holds the code repo URL, GitHub issue import and, in
  `multi` mode, the GitHub credential binding. **Workflow** holds states,
  types and priorities (`stalled` and `not_planned` cannot be removed) and the
  transition matrix: rows are where a card is, columns where it may go.
  **Automation** holds card defaults (what a new card's Automation rail starts
  with) and default skills. **Execution** holds the remote-execution worker
  images and the verify gate. **Danger** deletes the project. A tab with
  unsaved edits shows a dot until Save changes or Discard. In `multi` mode the
  wizard is admin-only and the settings page is read-only for non-admins.

## Playbooks

When Jira is configured, the "New playbook" form's title also takes a Jira epic
key or URL, and **Import from Jira** sits next to **Create playbook** (Enter
still creates). It is enabled only for a key or a URL on the configured site,
and disabled only while Jira rejects the token or a login CAPTCHA blocks it.
The import dialog shows the epic's children and their sub-tasks with checkboxes
(open, never-imported issues preselected; issues already in the playbook
disabled), a project picker per
issue plus one for all, an order-only warning on a sub-task placed in another
project than its parent, a warning when a target project's card prefix equals a
Jira project key in the import (PR links may attach to the wrong issue; it never
blocks), **Mark imported cards vetted** (off), **Include epic description in
each card** (on, with the epic text collapsed beneath it) and the playbook
title, read-only when the epic already has a playbook. When that playbook has an
active run, the dialog says so and Import stays disabled: stop the run or wait
for it to finish, then open the import again. The import runs in the
background: the dialog lists each issue's result as it lands (from the event
stream, caught up from the server on reconnect and every 5 s), then enables
**Open playbook**. An import that stops early, or that a server restart forgot,
lists the issues it never reached as `not run`. Closing the dialog does not
stop the import; opening the dialog for the epic again, on the same server, shows
that import's progress instead of offering Import, once the epic preview loads.
A playbook imported from Jira shows a Jira chip in its header that links to the
epic, and its cards carry a Jira source icon on the board. See
[Jira import](jira-import.md).

`/playbooks/:id` carries a **Run** card in its side panel. **Make runnable**
opens a confirm that lists what will change on the cards; the base-branch
dropdown offers the branches present in every repository the playbook spans
and names the branch the playbook branch is cut from ("Default branch" means
the repository default). A runnable playbook shows its branch, a status line
(`Running <card>, n of m`, `Waiting for you: <reason>`,
`Stopped: <reason>`, `Completed`), **Run** while idle and **Stop** while
active. A completed run whose entries are all complete keeps **Run** and
Make runnable disabled; entries added after completion re-enable both.
A completed run lists one "Open PR" compare link per repository.
Entry rows read `queued` while a run is active and show the waiting reason
on the run's entry (while a run is held on an unvetted card, on the first
incomplete entry). Removing the run's current entry, dragging any entry, or
unchecking a done manual entry is refused until the run is stopped; a
refused drag snaps the order back and a refused uncheck stays checked. The
toasts carry the server's reason. The list page shows a run pill. See
[playbooks](playbooks.md#running-a-playbook).

## Model selection

`/admin/model-selection` is the tier ladders page: one quality ladder per role
(coder, reviewer) over the live candidate catalog. Each column shows every
candidate as a pill at its prior with the tier bands and their counts; the bars
are draggable handles (coder handles on the left rail, reviewer handles at the
reviewer column's edge). The **linked** switch in the panel head starts on when
the two saved ladders are equal and off when they differ; when on, it moves the
same tier in both ladders and keeps them equal. The panel head also holds a
**Price headroom** number field (the best-value band multiplier, at least 1,
built-in 1.5) that is saved with the ladders and applied by the pick preview. A
row of provider checkboxes on its own line under the legend swatches (with
per-provider counts and `all` / `none` buttons) hides pills on both columns while a provider is
unchecked; the selection is remembered per browser and is display-only - it
never changes picks, seats, KPIs, band counts, saved ladders or any request. A
filled pill is the pick at its rung, a dashed outline a panel seat, a struck one a
blacklisted model. The pick preview beside it shows, per tier, the coder pick,
the reviewer pick and the three-seat review panel with prices, `↓ <rung>` when a
pick descended and `walked` on a seat that re-anchored its price band. A price
marked *list* is the Artificial Analysis list price for a model the gateway does
not price. A pill's tooltip names the AA row the candidate was scored from; the
panel meta line names the reasoning effort the gateway pins for its OpenAI
models when one is configured. The KPI row summarises the `complex` tier (reviewers clearing it,
the cheapest reviewer, the three-seat panel cost) plus the coder pick at
`moderate`. Nothing is sent until **Save**; the status pill in the header says
whether the next run uses the values on screen. The blacklist panel below lists
the models the agent reported incapable, with a per-row **Delist**.
Right-clicking a pill, a pick name in the preview, or a panel seat opens a
menu with **Add to blacklist**, or **Remove from blacklist** when the model is
already struck; either change refetches the catalog and re-runs the preview at
once, independently of the unsaved ladders. Details in
[model selection](model-selection.md#the-ladders-page).

## Keyboard shortcuts

Shortcuts are ignored while typing in a field and when a modifier is held.

| Key              | Action                                                           |
| ---------------- | ---------------------------------------------------------------- |
| `n`              | New card                                                         |
| `b`              | Back to the board                                                |
| `s`              | Project settings                                                 |
| `c`              | Toggle the execution console                                     |
| `1` to `9`       | Switch to the nth project in the sidebar (repo group, then name) |
| `Escape`         | Close the open panel; with none open, clear filters and search   |
| `Ctrl`/`Cmd`+`S` | Save the open card panel                                         |

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
