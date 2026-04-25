# Task skills

Curated Claude Code skills mounted into ContextMatrix worker containers at
`~/.claude/skills/`. Sub-agents engage them automatically when SKILL.md
descriptions match their work.

## Layout

One directory per skill at the repo root. Directory name = skill name
(referenced in `card.skills` and `project.default_skills`). Flat — no nesting.

```
go-development/SKILL.md
typescript-react/SKILL.md
...
```

## Description-writing convention

Descriptions drive auto-engagement. Anchor descriptions in **observable
activities and file types**, not subject areas.

| ✗ Topic-shaped (engages too eagerly) | ✓ Task-shaped (engages on real work)                                |
| ------------------------------------ | ------------------------------------------------------------------- |
| "Go programming guidance"            | "Use when implementing or modifying Go source files."               |
| "All things React"                   | "Use when writing or updating React/TypeScript components."         |
| "Documentation"                      | "Use when writing or updating documentation files (README, docs/)." |
| "Code review"                        | "Use when reviewing changes for correctness or security issues."    |

A topic-shaped description risks the orchestrator engaging a coding skill during
planning. A task-shaped description anchors engagement to the sub-agents
actually editing matching files.

## SKILL.md format

YAML frontmatter + markdown body.

```markdown
---
name: <skill-name-matching-dir-name>
description: Use when <observable activity>...
---

You are a <role>.

## When working on <activity>:

- Concrete pattern 1
- Concrete pattern 2
```

Optional frontmatter `allowed-tools: [Read, Write]` narrows the active tool set
when this skill is engaged. Never broadens.

## Editing

This is your repo. Edit, add, remove as needed. Push to your shared remote
(Gitea/GitHub) so the runner picks up changes — the runner does
`git pull --ff-only` before each `/trigger`.
