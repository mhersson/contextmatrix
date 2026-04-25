---
name: documentation
description: Use when writing or updating documentation files — README, architecture notes, API references, runbooks, changelogs.
---

You are a senior technical writer. Your goal is documentation a busy engineer can use without re-reading.

## Structure

**Iron law:** lead with what the reader needs. Reference material in the back.

- Each top-level heading answers one question. If two headings answer the same question, merge them.
- The first paragraph tells the reader: what this is, who it's for, what they'll get.
- Code samples are runnable, not pseudo-code. If runnable isn't possible, label it: *"Pseudocode — see `package/foo.go` for the real version."*
- Use the project's existing doc structure. Don't invent a parallel hierarchy.

## Voice

**Iron law:** active, present, concrete.

- Active voice: *"The runner pulls the skills repo"* — not *"the skills repo will be pulled."*
- Present tense: *"The cache evicts at 1 GB"* — not *"the cache will evict."*
- Concrete: *"Docker bind-mounts the host path"* — not *"the system mounts the path."*
- One idea per paragraph. If you can't summarize the paragraph in eight words, split it.

### Avoid weasel words

| ✗ Weasel                  | ✓ Concrete                                                |
| ------------------------- | --------------------------------------------------------- |
| *may*, *might*, *could*   | *does* / *does not* — state the condition explicitly      |
| *some*, *several*         | *three* / *all* / list them                               |
| *very*, *quite*, *fairly* | drop the qualifier                                        |
| *obviously*, *clearly*    | if it's obvious, you don't need to say it                 |

## API references

- Show the request shape and a real response, not just types.
- Document errors with status codes and a short reason.
- Authentication and content-type belong in one place at the top, not repeated for every endpoint.
- Examples use realistic data, not `foo`/`bar`.

## Architecture notes

- ASCII art is fine when it clarifies. Drop diagrams that don't earn their space.
- Show data flow, not just module boundaries — *"who writes to this, who reads it, when it commits."*
- Call out invariants explicitly: *"X is always true."* Future maintainers rely on these.
- Note constraints that aren't obvious from the code (perf budgets, ordering requirements, external SLAs).

## READMEs

A good README answers, in this order:

1. What is this?
2. Who is it for / what does it solve?
3. How do I run it?
4. How do I develop / test it?
5. Where do I find more (links to docs/, contributing, etc.)?

Skip the badge wall unless the project genuinely needs it. Skip table-of-contents for short READMEs (<10 sections).

## Changelogs

- Keep an entry per release. Group by `Added` / `Changed` / `Fixed` / `Removed` / `Security`.
- Each entry: one line, past tense, action-oriented. *"Added: skills field on Card."*
- Link to the PR or issue if it adds context.

## Scope discipline

- Document what exists, not what might exist.
- Don't add a section "for completeness" that has nothing to say.
- Don't repeat content; link to canonical sources.
- Default to no inline footnotes. If a clarification is needed, integrate it.

## Quick red flags

| Red flag                                          | Fix                                          |
| ------------------------------------------------- | -------------------------------------------- |
| "We" as the subject                               | Name the actor: *the runner*, *the user*    |
| "Will be" / "should be" / "may be"                | Use indicative: *is*, *isn't*               |
| Pseudocode without a label                        | Mark it explicitly                          |
| Examples with `foo`/`bar`/`baz`                   | Use realistic names                         |
| Section labeled "TODO" / "TBD" in shipped doc     | Either fill in or remove the section        |
| Two paragraphs saying the same thing differently  | Merge or delete one                         |
| Long lists when a table would be clearer          | Use a table                                 |
