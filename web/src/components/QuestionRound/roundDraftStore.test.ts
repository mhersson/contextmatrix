import { beforeEach, describe, expect, it, vi } from 'vitest';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound } from './parseRound';
import { DRAFT_CAP, DRAFT_STORAGE_KEY, clearDraft, loadDraft, pruneSession, saveDraft, type DraftState } from './roundDraftStore';

const store = new Map<string, string>();
const mock = {
  getItem: vi.fn((k: string) => store.get(k) ?? null),
  setItem: vi.fn((k: string, v: string) => void store.set(k, v)),
  removeItem: vi.fn((k: string) => void store.delete(k)),
  clear: vi.fn(() => store.clear()),
};
Object.defineProperty(globalThis, 'localStorage', { value: mock, configurable: true });

const scope = { sessionID: 's1', promptId: 'round:1' };
const roundOf = (extra: Record<string, unknown> = {}) => parseRound(rawRound({ assumptions: [{ id: 'A1', text: 'One cluster.' }], ...extra }))!;
const state = (over: Partial<DraftState> = {}): DraftState => ({
  draft: { picks: {}, decisions: {} },
  own: {},
  bases: { picks: {}, decisions: {} },
  ...over,
});
const map = () => JSON.parse(store.get(DRAFT_STORAGE_KEY) ?? '{}') as Record<string, unknown>;

beforeEach(() => {
  store.clear();
  vi.clearAllMocks();
});

describe('round draft storage', () => {
  it('restores picks, own answers and decisions made against what is still stored', () => {
    const round = roundOf();
    saveDraft(
      scope,
      round,
      state({
        draft: {
          picks: { 1: { kind: 'option', option: 2, title: 'Helm' }, 2: { kind: 'own', text: 'Loki only' } },
          decisions: { A1: true },
        },
        own: { 2: 'Loki only' },
        bases: { picks: { 1: null, 2: null }, decisions: { A1: null } },
      }),
    );
    const back = loadDraft(scope, round);
    expect(back.draft.picks[1]).toEqual({ kind: 'option', option: 2, title: 'Helm' });
    expect(back.draft.picks[2]).toEqual({ kind: 'own', text: 'Loki only' });
    expect(back.draft.decisions).toEqual({ A1: true });
    expect(back.own).toEqual({ 2: 'Loki only' });
  });

  it('drops an option pick whose option changed at that position', () => {
    saveDraft(scope, roundOf(), state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } }));
    const revised = roundOf({
      questions: [{ number: 1, title: 'Deploy tooling', text: 'Which?', options: [{ title: 'Kustomize' }, { title: 'Jsonnet' }] }],
    });
    expect(loadDraft(scope, revised).draft.picks).toEqual({});
  });

  it('drops a pick whose question the newest frame now stores, same value or not', () => {
    const picks = { 1: { kind: 'option', option: 2, title: 'Helm' } } as const;
    saveDraft(scope, roundOf(), state({ draft: { picks, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } }));
    expect(loadDraft(scope, roundOf({ answers: [{ number: 1, option: 2 }] })).draft.picks).toEqual({});
    expect(loadDraft(scope, roundOf({ answers: [{ number: 1, option: 1 }] })).draft.picks).toEqual({});
  });

  it('keeps a re-pick made against a stored answer that is still the same', () => {
    const stored = roundOf({ answers: [{ number: 1, option: 1 }] });
    saveDraft(
      scope,
      stored,
      state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: { number: 1, option: 1 } }, decisions: {} } }),
    );
    expect(loadDraft(scope, stored).draft.picks[1]).toEqual({ kind: 'option', option: 2, title: 'Helm' });
    expect(loadDraft(scope, roundOf({ answers: [{ number: 1, option: 2 }] })).draft.picks).toEqual({});
  });

  it('drops a decision the newest frame stores, and own text goes with its pick', () => {
    const round = roundOf();
    saveDraft(
      scope,
      round,
      state({
        draft: { picks: { 2: { kind: 'own', text: 'Loki' } }, decisions: { A1: true } },
        own: { 2: 'Loki' },
        bases: { picks: { 2: null }, decisions: { A1: null } },
      }),
    );
    const next = roundOf({ answers: [{ number: 2, text: 'Other' }], decisions: [{ id: 'A1', accept: false }] });
    const back = loadDraft(scope, next);
    expect(back.draft.decisions).toEqual({});
    expect(back.draft.picks).toEqual({});
    expect(back.own).toEqual({});
  });

  it('does not store a draft that is empty or already stored, and removes the entry', () => {
    const round = roundOf();
    saveDraft(scope, round, state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } }));
    expect(Object.keys(map())).toEqual(['s1/round:1']);
    saveDraft(scope, round, state());
    expect(map()).toEqual({});
  });

  it('clears one round and prunes a session down to the waiting round', () => {
    const round = roundOf();
    const s = state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } });
    saveDraft(scope, round, s);
    saveDraft({ sessionID: 's1', promptId: 'round:2' }, round, s);
    saveDraft({ sessionID: 's2', promptId: 'round:1' }, round, s);
    pruneSession('s1', 'round:2');
    expect(Object.keys(map()).sort()).toEqual(['s1/round:2', 's2/round:1']);
    pruneSession('s1', null);
    expect(Object.keys(map())).toEqual(['s2/round:1']);
    clearDraft({ sessionID: 's2', promptId: 'round:1' });
    expect(map()).toEqual({});
  });

  it('caps the whole map, evicting the oldest saved entry', () => {
    const round = roundOf();
    const s = state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } });
    let now = 1000;
    vi.spyOn(Date, 'now').mockImplementation(() => now++);
    for (let i = 0; i < DRAFT_CAP + 3; i++) saveDraft({ sessionID: 's', promptId: `p${i}` }, round, s);
    const keys = Object.keys(map());
    expect(keys).toHaveLength(DRAFT_CAP);
    expect(keys).not.toContain('s/p0');
    expect(keys).toContain(`s/p${DRAFT_CAP + 2}`);
    vi.restoreAllMocks();
  });

  it('works without storage: nothing throws and there is no saved draft', () => {
    const round = roundOf();
    const s = state({ draft: { picks: { 1: { kind: 'option', option: 2, title: 'Helm' } }, decisions: {} }, bases: { picks: { 1: null }, decisions: {} } });
    mock.getItem.mockImplementation(() => {
      throw new Error('blocked');
    });
    mock.setItem.mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(() => saveDraft(scope, round, s)).not.toThrow();
    expect(() => pruneSession('s1', null)).not.toThrow();
    expect(() => clearDraft(scope)).not.toThrow();
    expect(loadDraft(scope, round).draft.picks).toEqual({});
    mock.getItem.mockImplementation((k: string) => store.get(k) ?? null);
    mock.setItem.mockImplementation((k: string, v: string) => void store.set(k, v));
  });

  it.each(['{not json', '[1,2]', '{"s1/round:1":{"picks":5}}', '{"s1/round:1":null}'])('treats stored %s as no saved draft', (raw) => {
    store.set(DRAFT_STORAGE_KEY, raw);
    expect(loadDraft(scope, roundOf()).draft).toEqual({ picks: {}, decisions: {} });
    expect(() => saveDraft(scope, roundOf(), state())).not.toThrow();
  });
});
