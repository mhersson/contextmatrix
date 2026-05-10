# Refresh Knowledge Base

## Agent Configuration

This skill runs **inline on the invoking agent's session** — typically the
human's orchestrator (Opus). Step 5 spawns one Sonnet sub-agent per doc to
do the actual writing.

- Inline (orchestrator): runs in your current session
- Sub-agent (per doc): claude-sonnet-4-6

---

You are refreshing a project's knowledge base. The KB consists of up to four
markdown docs per repo (`architecture.md`, `code-structure.md`,
`api-documentation.md`, `glossary.md`) stored in the boards repo at
`<project>/knowledge/<repo>/`. Refresh produces fresh AI-generated content
and commits it atomically.

## Inputs

- `project` (required): project name.
- `repo` (optional): refresh only this repo. If omitted: primary repo for
  multi-repo projects, or the only repo for single-repo projects.
- `--plan`: dry-run; print the build plan and exit without producing docs.

## Step 1: Get the build plan

Call `refresh_knowledge_base` MCP tool with `project`, `repo` (if given),
and `agent_id: <the human: identity for this session — same one you have been using on previous tool calls>`.
If you do not have a human-prefixed identity for this session, ask the user once and use their answer.
Returns a plan listing each doc to rebuild, its reason, current `human_edited` flag, and cost estimate.

If `--plan` was passed, print the plan and exit.

## Step 2: Confirm with the user

Print the plan. For each doc with `human_edited: true`, ask:

> `<repo>/<doc>` has been edited by a human since the last AI build.
> Overwrite? (skip / overwrite). Default: skip.

Build the rebuild set: all flagged-false docs plus any flagged-true docs
the user explicitly approved.

If the rebuild set is empty, print "Nothing to rebuild." and exit.

## Step 3a: Deduplicate plan items by repo

The plan returns one item per (repo, doc) pair. Multiple items may share a repo.
Build a deduplicated list of repos to clone:

    repos_to_clone = unique(item.repo for item in plan.items)

For each repo in `repos_to_clone`, perform Steps 3b–3c (clone + capture HEAD)
once. In Step 5, spawn one sub-agent per (repo, doc) pair from the original
plan.items.

## Step 3b: Locate or clone each repo

For each repo in `repos_to_clone`:

1. Read the project's `repos` list via `list_projects` and find the matching
   entry. Take the URL from there.
2. Clone shallowly to `/tmp/cm-knowledge-<project>/<repo>/` (depth 1). If
   the directory already exists from a previous run, `git fetch` and reset
   to the remote default branch.
3. Capture HEAD SHA: `git -C /tmp/cm-knowledge-<project>/<repo>/ rev-parse HEAD`.
   Save this as `head_commit` for the commit step.
4. **Do not modify the target repo. Read-only.**

## Step 4: Discovery pass (run once per repo before generating any doc)

Before invoking sub-agents, scan the cloned repo to surface concrete
inputs the doc generators will need. Record the findings in your own
working memory; pass them to each sub-agent below.

### 4.1 Package / module discovery

- List top-level directories that look like packages or modules.
- Identify entrypoints: `main.go`, `cmd/*/main.go`, `index.ts`, `src/main.tsx`,
  `setup.py`, `pyproject.toml`, etc.
- Classify each package: **Application**, **Infrastructure** (CDK/Terraform/Helm),
  **Library**, **Client**, **Test**.

### 4.2 Build system discovery

- Detect: `go.mod`, `package.json`, `pyproject.toml`/`setup.py`, `Cargo.toml`,
  `Makefile`, `build.gradle`, `pom.xml`, `Brazil`-style configs.
- Note relevant scripts (`make build`, `npm run build`, `go build ./...`).

### 4.3 Service / surface discovery

- HTTP handlers: search for `http.HandleFunc`, `app.get(`, `@app.route`,
  `mux.Handle`, framework-specific decorators.
- MCP tools: `mcp.AddTool`, tool registration files.
- CLI: cobra commands, click commands, `argparse` parsers.
- Background jobs / workers, message consumers, schedulers.
- Webhook endpoints.

### 4.4 Data store discovery

- Database drivers / ORMs in dependencies.
- Schema/migration directories.
- File storage, caches, message queues.

### 4.5 External integration discovery

- Third-party SDK imports.
- Outbound HTTP clients with notable host hints.
- Auth mechanisms (OAuth, API keys, JWT).

### 4.6 Convention discovery

- Linter / formatter configs (`.golangci.yml`, `.eslintrc`, `pyproject.toml [tool.ruff]`).
- Test framework (`testing` + `testify`, `vitest`, `pytest`).
- Documentation in `CLAUDE.md`, `AGENTS.md`, `docs/`, `README.md` — read the
  index of `docs/` if present.

Record this discovery once and reuse it across the per-doc sub-agents.
Do NOT re-walk the codebase from each sub-agent.

## Step 5: Generate each doc

For each `(repo, doc)` in the rebuild set, spawn a focused Sonnet sub-agent
via the Task tool. Pass:

- The discovery findings from Step 4.
- The current doc content (read via `read_knowledge_doc`) for continuity if
  any exists.
- The path to the cloned repo and read-only access.
- The target output template (below) — instruct the sub-agent to produce
  content matching that template *as a whole markdown file*. The server
  replaces the file entirely.

Collect each sub-agent's output as the new doc content.

### 5.1 `architecture.md` template

```markdown
# System Architecture

## System Overview

[2-4 paragraph high-level description of what the system does, who calls
it, and what its role is in the broader environment. Avoid restating
README content verbatim.]

## Architecture Diagram

[ASCII diagram showing top-level packages, external services, data stores,
and the relationships between them. Use the ASCII style described in the
"Diagrams" section below. Keep node labels short. Do not include
implementation details.]

## Component Descriptions

### [Package or component name, exactly as named in the codebase]

- **Purpose**: [What it owns; the question it answers.]
- **Responsibilities**: [Bulleted list of concrete responsibilities.]
- **Dependencies**: [Other internal packages it relies on.]
- **Public surface**: [Exported types/functions a consumer can rely on.]
- **Type**: [Application | Library | Infrastructure | Client | Test]

[Repeat for each significant package. Skip trivial packages.]

## Data Flow

[ASCII vertical-flow diagram for one or two key workflows — e.g., "user
creates a card", "agent claims and executes". Show the call path across
packages, not within a package. Use the vertical-flow style from the
"Diagrams" section below.]

## Integration Points

- **External APIs**: [Each with purpose and where it's called from.]
- **Databases / data stores**: [Each with purpose and ownership.]
- **Message queues, webhooks, third-party services**: [Each with purpose.]

## Notable design decisions

[Short bulleted list: choices that would surprise a new contributor or
constrain future changes. Reference the file/package where the decision
is enforced.]
```

### 5.2 `code-structure.md` template

```markdown
# Code Structure

## Build System

- **Type**: [go modules / npm / pyproject / cargo / make-driven / etc.]
- **Key files**: [`go.mod`, `Makefile`, `package.json`, etc.]
- **Common commands**: [The 3-5 commands a contributor will actually run:
  build, test, lint, run.]

## Module / package hierarchy

[Structured indented list showing the top-level directories and their
child packages. One line per package. ASCII tree style is fine if the
hierarchy benefits from visual grouping.]

## Existing files inventory

[For each significant source file or directory, one line:
`path/to/file.ext` — [purpose / responsibility].

Skip generated code, vendored dependencies, lock files. Group by package.
This is the doc that prevents agents from creating files in wrong places.]

## Design patterns

### [Pattern name — e.g., "Service-layer mutex for write serialization"]

- **Where**: [File path or package.]
- **Why**: [The problem this solves in this codebase.]
- **How**: [One-paragraph implementation summary; link to the canonical
  example by file:line.]

[Repeat for each established pattern: error handling style, dependency
injection convention, testing pattern, concurrency model, etc.]

## Critical dependencies

### [Dependency name]

- **Version**: [from lockfile]
- **Used in**: [Packages that import it.]
- **Why**: [What it provides; what would change if it were swapped.]

[Limit to dependencies whose presence/absence would meaningfully change
how new code is written. Skip transitive utilities.]

## Naming conventions

[Short bulleted list: how packages, types, files, tests, and exported
identifiers are named. Reference 1-2 canonical examples.]
```

### 5.3 `api-documentation.md` template

**Skip this doc entirely if the repo has no public surface** (libraries
without REST/MCP/CLI). Note the skip in your output to the user.

```markdown
# API Documentation

## Overview

[Which surfaces this repo exposes: REST, MCP, CLI, gRPC, webhooks. Where
each is registered in the codebase.]

## REST endpoints

### `[METHOD] /path/{param}`

- **Purpose**: [What it does; who calls it.]
- **Auth**: [How authentication is enforced; required headers.]
- **Request body**: [Shape; required fields; example.]
- **Response**: [Status codes; body shape; example.]
- **Errors**: [Notable error codes/conditions.]

[Repeat for each endpoint. Group by resource.]

## MCP tools

### `tool_name`

- **Purpose**: [What it does.]
- **Auth constraints**: [Human-only? Agent-only? `agent_id` checks?]
- **Input**: [Schema fields with types and required-ness.]
- **Output**: [Shape.]

[Repeat for each tool.]

## CLI

### `binary subcommand [args]`

- **Purpose**: [What it does.]
- **Flags**: [Important flags with descriptions.]
- **Outputs**: [Stdout shape, exit codes, side effects.]

[Repeat for each subcommand.]

## Data models

### [Model name]

- **Shape**: [Fields with types.]
- **Constraints / validation rules**: [What's enforced where.]
- **Related models**: [Linkages.]

[Limit to models that cross API boundaries.]
```

### 5.4 `glossary.md` template

```markdown
# Glossary

## Domain terms

### [Term]

[1-3 sentence definition in this project's specific sense. If the term has
a generic meaning that differs from this project's, call out the
difference.]

[Repeat for each project-specific term. Alphabetize.]

## Naming conventions

[Bulleted list: identifier conventions that aren't obvious from a single
file. Examples: prefix conventions for IDs, naming pattern for skill
files, project-specific suffixes/prefixes.]

## Abbreviations and acronyms

| Abbreviation | Expansion | Notes |
|---|---|---|
| [ABBR] | [Full name] | [Where used.] |

## Terms NOT to use

[Bulleted list: terms that look domain-y but mean something different in
this project, or terms that are ambiguous across teams. Call out the
preferred term to use instead.]
```

## Step 6: Commit atomically

Per repo, call `commit_knowledge_docs` once with the full map:

```
{
  project: "<project>",
  repo: "<repo>",
  head_commit: "<SHA from step 3b>",
  agent_id: <the human: identity for this session>,
  docs: {
    "architecture.md": "<full new content>",
    ...
  }
}
```

Single atomic commit. Report the returned commit SHA and file list to the
user.

## Token usage

Refresh runs outside any card context, so `report_usage` (which requires
`card_id`) is not applicable. Token consumption from the Sonnet sub-agents
is not tracked centrally for this skill — surface the rough total to the
user in your final summary if useful, but do not call `report_usage`.

## Constraints

- Never modify the target code repo.
- Never write to disk outside `/tmp/cm-knowledge-*/` and the boards repo
  (mediated by MCP).
- ASCII diagrams only — no mermaid. See "Diagrams" below for the style.

## Diagrams

ASCII only. Use these characters and patterns; no Unicode box-drawing
(`┌`, `─`, etc. render inconsistently across fonts).

**Allowed characters:** `+` `-` `|` `^` `v` `<` `>` and alphanumeric text.

**Box rule:** every line in a box must have exactly the same character
count, including spaces. Verify by eye that corners align in a
monospace font.

**Box pattern:**

```
+----------------------------+
|     Component Name         |
|  Short description text    |
+----------------------------+
```

**Horizontal flow:**

```
+--------+      +---------+      +---------+
| Source | ---> | Process | ---> |  Sink   |
+--------+      +---------+      +---------+
```

**Vertical flow with edge labels:**

```
+----------+
|  Input   |
+----------+
     |
     | validates
     v
+----------+
| Process  |
+----------+
     |
     | persists
     v
+----------+
|  Store   |
+----------+
```

**Nested boxes** (use sparingly):

```
+--------------------------------------+
|         Outer Component              |
|  +-------------------------------+   |
|  |       Inner Component         |   |
|  +-------------------------------+   |
+--------------------------------------+
```

Keep diagrams small. If a diagram needs more than ~15 lines or boxes,
prose plus a few small diagrams is clearer than one large diagram.
