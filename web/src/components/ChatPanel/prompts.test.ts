import { describe, it, expect } from 'vitest';
import { derivePromptStates, matchReply, parsePromptEntry, type PromptState } from './prompts';
import type { LogEntry } from '../../types';
import { rawRound, roundEntry } from '../../test-utils/roundFixtures';

const GATE = [
  { label: 'Approve', reply: 'approve' },
  { label: 'Reject', reply: 'reject', feedback: true },
];
const OPTIONS = [
  { label: 'Postgres', reply: 'Postgres' },
  { label: 'SQLite', reply: 'SQLite' },
];
// The design gate with slicing folded into it: two feedback replies share a first word.
const FOLDED = [
  { label: 'Approve', reply: 'approve' },
  { label: 'Reject', reply: 'reject', feedback: true },
  { label: 'Re-slice', reply: 'reject slicing', feedback: true },
];
const POLICY = [
  { label: 'Gate design auto', reply: 'gate solution_design auto' },
];

function prompt(seq: number, actions: unknown[], extra: Record<string, unknown> = {}): LogEntry {
  return {
    ts: '2026-10-16T10:00:00Z',
    card_id: '',
    type: 'text',
    kind: 'prompt',
    seq,
    content: JSON.stringify({ text: `prompt ${seq}`, prompt: { id: `P${seq}`, actions, ...extra } }),
  };
}

function user(seq: number, content: string): LogEntry {
  return { ts: '2026-10-16T10:00:00Z', card_id: '', type: 'user', content, seq };
}

function text(seq: number, content: string): LogEntry {
  return { ts: '2026-10-16T10:00:00Z', card_id: '', type: 'text', content, seq };
}

describe('parsePromptEntry', () => {
  it('reads the envelope text, id, free-text flag and actions', () => {
    expect(parsePromptEntry(prompt(1, GATE, { free_text: true }))).toEqual({
      text: 'prompt 1',
      prompt: {
        id: 'P1',
        freeText: true,
        actions: [
          { label: 'Approve', reply: 'approve', feedback: false, recommended: false },
          { label: 'Reject', reply: 'reject', feedback: true, recommended: false },
        ],
      },
    });
  });

  it('reads the display text, kind and recommended flag, only when well-typed', () => {
    const e: LogEntry = {
      ...prompt(1, []),
      content: JSON.stringify({
        text: 'full',
        prompt: {
          kind: 'question',
          text: 'just the question',
          actions: [
            { label: 'A', reply: 'a', recommended: true },
            { label: 'B', reply: 'b', recommended: 'yes' },
          ],
        },
      }),
    };
    const env = parsePromptEntry(e);
    expect(env?.prompt.text).toBe('just the question');
    expect(env?.prompt.kind).toBe('question');
    expect(env?.prompt.actions.map((a) => a.recommended)).toEqual([true, false]);

    const bad: LogEntry = { ...prompt(2, []), content: JSON.stringify({ text: 't', prompt: { kind: '', text: 5, actions: [] } }) };
    expect(parsePromptEntry(bad)?.prompt.text).toBeUndefined();
    expect(parsePromptEntry(bad)?.prompt.kind).toBeUndefined();
  });

  it('drops malformed actions and reads missing actions as none', () => {
    const mixed = prompt(1, [
      { label: '', reply: 'x' },
      { label: 'Ok', reply: '' },
      { label: 'Go', reply: 'go', feedback: 'yes' },
      'junk',
    ]);
    expect(parsePromptEntry(mixed)?.prompt.actions).toEqual([{ label: 'Go', reply: 'go', feedback: false, recommended: false }]);

    const none: LogEntry = { ...prompt(2, []), content: JSON.stringify({ text: 't', prompt: { actions: null } }) };
    expect(parsePromptEntry(none)?.prompt.actions).toEqual([]);
  });

  it('returns null for a row that is not a prompt or an envelope that does not parse', () => {
    expect(parsePromptEntry(text(1, '{"text":"t","prompt":{}}'))).toBeNull();
    for (const content of ['not json', '[]', '{"text":1,"prompt":{}}', '{"text":"t"}', '{"text":"t","prompt":[]}']) {
      expect(parsePromptEntry({ ...prompt(1, GATE), content })).toBeNull();
    }
  });
});

describe('matchReply', () => {
  const gate = parsePromptEntry(prompt(1, GATE))!.prompt;

  it.each([
    ['approve', 'Approve'],
    ['  APPROVE ', 'Approve'],
    ['reject needs a cache layer', 'Reject'],
    ['Reject\nneeds a cache layer', 'Reject'],
    ['reject', null],
    ['rejection', null],
    ['approve it', null],
    ['what about cost?', null],
  ])('%j selects %s', (reply, label) => {
    expect(matchReply(gate, reply)?.label ?? null).toBe(label);
  });

  const folded = parsePromptEntry(prompt(2, FOLDED))!.prompt;

  it.each([
    ['reject slicing merge them', 'Re-slice'],
    ['Reject Slicing\nmerge them', 'Re-slice'],
    ['reject too broad', 'Reject'],
    ['reject slicing', 'Reject'],
    ['approve', 'Approve'],
  ])('on the folded gate %j selects %s (longest reply wins)', (reply, label) => {
    expect(matchReply(folded, reply)?.label ?? null).toBe(label);
  });

  const policy = parsePromptEntry(prompt(3, POLICY))!.prompt;

  it.each([
    ['gate solution_design auto', 'Gate design auto'],
    ['  Gate Solution_Design AUTO ', 'Gate design auto'],
    ['gate solution_design', null],
    ['gate solution_design auto now', null],
  ])('a multi-word reply %j selects %s', (reply, label) => {
    expect(matchReply(policy, reply)?.label ?? null).toBe(label);
  });
});

describe('derivePromptStates', () => {
  const cases: { name: string; logs: LogEntry[]; live?: boolean; want: (PromptState | undefined)[] }[] = [
    { name: 'an unanswered gate is open while live', logs: [prompt(1, GATE)], want: [{ status: 'open' }] },
    {
      name: 'an unanswered gate is paused while not live',
      logs: [prompt(1, GATE)],
      live: false,
      want: [{ status: 'paused' }],
    },
    {
      name: 'a matching reply answers it',
      logs: [prompt(1, GATE), user(2, 'approve')],
      want: [{ status: 'answered', label: 'Approve' }, undefined],
    },
    {
      name: 'a reject with feedback answers it',
      logs: [prompt(1, GATE), user(2, 'reject needs a cache layer')],
      want: [{ status: 'answered', label: 'Reject' }, undefined],
    },
    {
      name: 'the longest matching reply names the answer',
      logs: [prompt(1, FOLDED), user(2, 'reject slicing merge them')],
      want: [{ status: 'answered', label: 'Re-slice' }, undefined],
    },
    {
      name: 'a multi-word reply answers it',
      logs: [prompt(1, POLICY), user(2, 'gate solution_design auto')],
      want: [{ status: 'answered', label: 'Gate design auto' }, undefined],
    },
    {
      name: 'a bare reject leaves it open',
      logs: [prompt(1, GATE), user(2, 'reject')],
      want: [{ status: 'open' }, undefined],
    },
    {
      name: 'a note leaves it open',
      logs: [prompt(1, GATE), user(2, 'what about cost?')],
      want: [{ status: 'open' }, undefined],
    },
    {
      name: 'any reply answers a free-text question',
      logs: [prompt(1, OPTIONS, { free_text: true }), user(2, 'MySQL, please')],
      want: [{ status: 'answered', label: null }, undefined],
    },
    {
      name: 'a free-text reply that matches an option names it',
      logs: [prompt(1, OPTIONS, { free_text: true }), user(2, ' postgres ')],
      want: [{ status: 'answered', label: 'Postgres' }, undefined],
    },
    {
      name: 'a later prompt supersedes one still open',
      logs: [prompt(1, GATE), user(2, 'what about cost?'), prompt(3, GATE)],
      want: [{ status: 'superseded' }, undefined, { status: 'open' }],
    },
    {
      name: 'an answered prompt stays answered after a later prompt',
      logs: [prompt(1, GATE), user(2, 'approve'), prompt(3, GATE)],
      want: [{ status: 'answered', label: 'Approve' }, undefined, { status: 'open' }],
    },
    {
      name: 'a reply before the prompt does not answer it',
      logs: [user(1, 'approve'), prompt(2, GATE)],
      want: [undefined, { status: 'open' }],
    },
    {
      name: 'the first answering reply wins',
      logs: [prompt(1, GATE), user(2, 'approve'), user(3, 'reject too slow')],
      want: [{ status: 'answered', label: 'Approve' }, undefined, undefined],
    },
    {
      name: 'assistant text between the prompt and the reply changes nothing',
      logs: [prompt(1, GATE), text(2, 'thinking out loud'), user(3, 'approve')],
      want: [{ status: 'answered', label: 'Approve' }, undefined, undefined],
    },
    {
      name: 'a prompt row that does not parse neither opens nor supersedes',
      logs: [prompt(1, GATE), { ...prompt(2, GATE), content: 'oops' }],
      want: [{ status: 'open' }, undefined],
    },
  ];

  it.each(cases)('$name', ({ logs, live = true, want }) => {
    const states = derivePromptStates(logs, live);
    expect(logs.map((e) => states.get(e))).toEqual(want);
  });
});

describe('round prompts', () => {
  it('parses a round only on a kind "round" prompt with an id', () => {
    expect(parsePromptEntry(roundEntry(1, 'round:1', rawRound()))?.prompt.round?.questions).toHaveLength(3);
    expect(parsePromptEntry(roundEntry(2, '', rawRound()))?.prompt.round).toBeUndefined();
    const asQuestion: LogEntry = {
      ...roundEntry(3, 'Q1', rawRound()),
      content: JSON.stringify({ text: 't', prompt: { id: 'Q1', kind: 'question', round: rawRound() } }),
    };
    expect(parsePromptEntry(asQuestion)?.prompt.round).toBeUndefined();
  });

  it('closes an unanswered prompt but never becomes a prompt state itself', () => {
    const q = prompt(1, OPTIONS, { kind: 'question', free_text: true });
    const r = roundEntry(2, 'round:1', rawRound());
    const u = user(3, 'Does Q3 change Q1?');
    const states = derivePromptStates([q, r, u], true);
    expect(states.get(q)).toEqual({ status: 'superseded' });
    expect(states.has(r)).toBe(false);
  });
});

describe('derivePromptStates command rows', () => {
  it('leaves an open free-text prompt open across a command row', () => {
    const cmd: LogEntry = { ...user(2, 'cap 4.00'), kind: 'command' };
    const p = prompt(1, GATE, { free_text: true });
    expect(derivePromptStates([p, cmd], true).get(p)).toEqual({ status: 'open' });
  });

  it('leaves a matching-reply prompt open when the command row echoes its reply', () => {
    const cmd: LogEntry = { ...user(2, 'approve'), kind: 'command' };
    const p = prompt(1, GATE);
    expect(derivePromptStates([p, cmd], true).get(p)).toEqual({ status: 'open' });
  });
});
