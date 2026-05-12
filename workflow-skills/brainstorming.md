# Brainstorming

## Agent Configuration

- **Model:** claude-sonnet-4-6 — runs inline inside the create-plan
  orchestrator session.

---

Run inline within the create-plan orchestrator session. Turn the card's
stated intent into a fully-formed design through dialogue with the user,
then update the card body with the agreed `## Design`. Do NOT transition
the card. Do NOT invoke other skills. Do NOT spawn sub-agents — sub-agents
have no chat channel back to the user. When done, control returns to
create-plan.

## Safety check

If no user channel is available, return immediately.

## Log engagement (first action)

Once, before opening the dialogue, call:

```
add_log(card_id=<parent_id>, agent_id=<your_agent_id>,
        action='skill_engaged', message='engaged brainstorming')
```

## Heartbeat

- Before prompting the user at any gate: call `heartbeat` + `report_usage`.
- On resume (first tool call after the user's reply): call `heartbeat`.
  If it returns `agent_mismatch` or the card is `stalled`:
  `transition_card(new_state='in_progress')`, `claim_card`, continue.

## HARD-GATE

If create-plan invoked you, the card has already been gated as
creative work that warrants design discussion. Complete the process
— present a design and get user approval before returning. Do not
bail out mid-flight because the card seems simple once you start
reading it.

## Anti-Pattern: "This Card Is Simpler Than I Thought, I'll Skip Ahead"

create-plan filters out non-creative cards (bugs, chores, refactors,
dependency bumps, cards labelled `simple`) before invoking you. If
you're running, the card needs design. Small creative work — a single
function, a UI tweak, a config change — still benefits from a
confirmed design. The design can be short (a few sentences), but you
MUST present it and get the user's confirmation before returning.

## Step 0: Design already complete?

Read the card body via `get_card`. If the body already contains a
substantial `## Design` section (a previous brainstorming pass, or a
thoroughly-written initial description), present a brief summary of
the existing design and ask:

> "The card already has a design section. Want me to walk through it
>  together, or proceed straight to planning?"

- **User picks "proceed straight to planning":** return immediately.
  Control passes to create-plan Phase 1.
- **User wants to walk through:** do a focused review pass — any gaps,
  ambiguities, or new requirements? Update the body via `update_card`
  if anything changes, get user confirmation, then return.

If the body has no design section, proceed with the full process below.

## Checklist

You MUST complete each of these in order:

1. **Load project knowledge base** — call `get_knowledge_base` with
   `project=<this card's project>`. Immediately after the call returns, log
   the outcome:

   ```
   add_log(card_id=<parent_id>, agent_id=<your_agent_id>,
           action='kb_loaded',
           message='loaded N docs' OR 'no KB built yet')
   ```

   If docs are returned, treat them as authoritative architectural
   context: use `code-structure.md` to choose file paths,
   `architecture.md` to honour component boundaries,
   `api-documentation.md` to avoid breaking public surfaces, and
   `glossary.md` to use the project's vocabulary correctly. Reference
   them when discussing architecture, decomposition, or naming. If
   empty, note that to the user when relevant and proceed.

   If `summaries` is non-empty, use each doc's summary to judge
   relevance to the current task before loading its full content from
   `docs`. Retain in active context only the docs whose summary
   indicates relevance. If `summaries` is empty or a doc has no entry,
   load all docs.
2. **Explore project context** — read files referenced in the card and
   anything the KB doesn't cover (recent commits, files mentioned in
   the body). Don't re-derive what the KB already states.
3. **Ask clarifying questions** — one at a time, understand purpose,
   constraints, success criteria.
4. **Propose 2–3 approaches** — with trade-offs and your recommendation.
5. **Present design** — in sections scaled to their complexity, get
   user approval after each section.
6. **Update card body** — via `update_card`, add or replace a
   `## Design` section with the agreed design.
7. **Description self-review** — quick inline check for placeholders,
   contradictions, ambiguity, scope (see below); fix and re-update.
8. **User confirms updated body** — last gate before returning.
9. **Return** — control passes back to create-plan Phase 1 Step 2 (Draft).

## The Process

**Understanding the idea:**

- Read the card body first via `get_card` — that's the user's stated intent.
- Check related files, project architecture docs, recent commits.
- Before asking detailed questions, assess scope: if the card describes
  multiple independent subsystems (e.g., "build a feature with new API,
  new UI, new background worker, and new docs"), flag this immediately.
  Don't refine details of a card that should be split into multiple cards.
- If the card is too large for a single design, help the user decompose
  into sibling cards: what are the independent pieces, how do they
  relate, what order should they be built? Then brainstorm the first
  piece through the normal flow.
- For appropriately-scoped cards, ask questions one at a time.
- Prefer multiple-choice questions when possible; open-ended is fine too.
- Only one question per message — if a topic needs more exploration, break
  it into multiple questions.
- Focus on understanding: purpose, constraints, success criteria.

**Exploring approaches:**

- Propose 2–3 different approaches with trade-offs.
- Present options conversationally with your recommendation and reasoning.
- Lead with your recommended option and explain why.

**Presenting the design:**

- Once you believe you understand what you're building, present the design.
- Scale each section to its complexity: a few sentences if straightforward,
  up to 200–300 words if nuanced.
- Ask after each section whether it looks right so far.
- Cover: architecture, components, data flow, error handling, testing.
- Be ready to go back and clarify if something doesn't make sense.

**Design for isolation and clarity:**

- Break the system into smaller units that each have one clear purpose,
  communicate through well-defined interfaces, and can be understood and
  tested independently.
- For each unit, you should be able to answer: what does it do, how do
  you use it, what does it depend on?
- Can someone understand what a unit does without reading its internals?
  Can you change the internals without breaking consumers? If not, the
  boundaries need work.
- Smaller, well-bounded units are also easier for an agent to work with
  — agents reason better about code they can hold in context at once,
  and edits are more reliable when files are focused.

**Working in existing codebases:**

- Explore the current structure before proposing changes. Follow existing
  patterns.
- Where existing code has problems that affect the work (e.g., a file
  that's grown too large, unclear boundaries, tangled responsibilities),
  include targeted improvements as part of the design.
- Don't propose unrelated refactoring. Stay focused on what serves the
  current card.

## After the Design

**Updating the card:**

- Use `update_card(card_id=<parent_id>, body=<new body>)` to add or
  replace a `## Design` section in the card body. Keep all existing
  content (title, description, prior sections); only the design portion
  is new or refreshed.

**Description Self-Review:**

After updating the card, look at the new body with fresh eyes:

1. **Placeholder scan:** Any "TBD", "TODO", incomplete sections, or vague
   requirements? Fix them via another `update_card`.
2. **Internal consistency:** Do any sections contradict each other? Does
   the architecture match the feature description?
3. **Scope check:** Is this focused enough for a single implementation
   plan, or does it need decomposition into sibling cards?
4. **Ambiguity check:** Could any requirement be interpreted two
   different ways? If so, pick one and make it explicit.

Fix any issues inline. No need to re-review — just fix and move on.

**User Confirmation:**

After the self-review, ask the user to confirm the updated card body:

> "Card description updated with the agreed design. Please confirm —
>  any last changes before I hand back to create-plan to draft the
>  subtasks?"

Heartbeat before prompting. Heartbeat on resume.

If the user requests changes, make them via another `update_card` and
re-confirm. Only return once the user approves.

**Return:**

When the user confirms, stop talking. Do NOT print a structured handoff message.
Do NOT transition the card.

## Key Principles

- **One question at a time** — don't overwhelm with multiple questions.
- **Multiple choice preferred** — easier to answer than open-ended when possible.
- **YAGNI ruthlessly** — remove unnecessary features from all designs.
- **Explore alternatives** — always propose 2–3 approaches before settling.
- **Incremental validation** — present design, get approval before moving on.
- **Be flexible** — go back and clarify when something doesn't make sense.
- **The card is the spec** — never write the design to a separate file.

## Anti-Patterns

- **"This card is too simple to need design"** — every card goes through
  this. The design can be one sentence for trivial work, but it must
  exist and the user must confirm it.
- **"I'll just draft the plan and let create-plan figure it out"** —
  that's exactly what Phase 0 exists to prevent. Brainstorm first.
- **Spawning sub-agents** — you have no channel to the user from a
  sub-agent. Run inline.
- **Transitioning the card** — that's create-plan's responsibility.
- **Invoking another skill** — return and let create-plan continue.
- **Writing the design to a separate file** — the card body IS the spec.
