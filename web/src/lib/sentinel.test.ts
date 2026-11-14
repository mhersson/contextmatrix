import { describe, it, expect } from 'vitest';
import type {
  SentinelCardView,
  SentinelFindingView,
  SentinelHealView,
  SentinelRunView,
  SentinelState,
  SentinelStatus,
} from '../types';
import {
  cardStatus,
  chipText,
  findingLabel,
  findingOutcome,
  lineDiff,
  pillTone,
  sentinelSpend,
  spendLine,
  splitFindings,
  verifyLines,
  stripText,
  stripTone,
  windowLines,
  type CardStatus,
  type PillTone,
} from './sentinel';

function status(over: Partial<SentinelStatus> = {}): SentinelStatus {
  return {
    enabled: true,
    state: 'watching',
    mode: 'endpoint',
    healing: 0,
    analysing: 0,
    healed_today: 0,
    spent_today_usd: 0,
    daily_cap_usd: 20,
    headroom: 0.85,
    ...over,
  };
}

describe('stripText', () => {
  it.each<[Partial<SentinelStatus>, string]>([
    [{ state: 'watching' }, 'watching'],
    [{ state: 'working', healing: 2 }, 'healing 2'],
    [{ state: 'working', healing: 2, analysing: 3 }, 'healing 2 · analysing 3'],
    [{ state: 'working', healing: 0, analysing: 3 }, 'analysing 3'],
    [{ state: 'working', healing: 1, analysing: 0 }, 'healing 1'],
    [{ state: 'paused', reason: 'daily cap reached' }, 'daily cap reached'],
    [{ state: 'not_running', reason: 'controller stopped' }, 'not running'],
  ])('%j -> %s', (over, want) => {
    expect(stripText(status(over))).toBe(want);
  });
});

describe('stripTone', () => {
  it.each<[SentinelState, string]>([
    ['watching', 'active'],
    ['working', 'active'],
    ['paused', 'paused'],
    ['not_running', 'stopped'],
  ])('%s -> %s', (state, want) => {
    expect(stripTone(status({ state }))).toBe(want);
  });
});

describe('spendLine', () => {
  it('formats spend against the cap', () => {
    expect(spendLine(status({ spent_today_usd: 1.2, daily_cap_usd: 20 }))).toBe('$1.20 of $20.00 today');
  });
});

describe('windowLines', () => {
  it('formats each window against the threshold', () => {
    const s = status({ mode: 'claude_code', windows: [{ window: 'five_hour', utilization: 0.62 }] });
    expect(windowLines(s)).toEqual(['five_hour 62% of 85%']);
  });
  it('is empty without windows', () => {
    expect(windowLines(status())).toEqual([]);
  });
});

describe('lineDiff', () => {
  it.each<[string, string, string, [string, string][]]>([
    ['replaced line', 'a\nb', 'a\nc', [['same', 'a'], ['del', 'b'], ['add', 'c']]],
    ['identical', 'a\nb', 'a\nb', [['same', 'a'], ['same', 'b']]],
    ['from empty', '', 'a', [['add', 'a']]],
    ['to empty', 'a', '', [['del', 'a']]],
    ['insert in the middle', 'a\nc', 'a\nb\nc', [['same', 'a'], ['add', 'b'], ['same', 'c']]],
    ['both empty', '', '', []],
    ['section body edge blanks', '\nold\nkeep\n', '\nnew\nkeep\n', [['del', 'old'], ['add', 'new'], ['same', 'keep']]],
    ['blank-only text', '\n\n', 'a', [['add', 'a']]],
    ['inner blanks kept', 'a\n\nb', 'a\n\nc', [['same', 'a'], ['same', ''], ['del', 'b'], ['add', 'c']]],
  ])('%s', (_name, before, after, want) => {
    expect(lineDiff(before, after).map((l) => [l.kind, l.text])).toEqual(want);
  });
});

describe('verifyLines', () => {
  it('is empty when absent and lists the set fields otherwise', () => {
    expect(verifyLines(undefined)).toBe('');
    expect(verifyLines({ command: 'make test', timeout_seconds: 60, env: ['A', 'B'] })).toBe(
      'command: make test\ntimeout_seconds: 60\nenv: A, B',
    );
  });
});

describe('chipText', () => {
  it('keeps the working counts and shortens a pause', () => {
    expect(chipText(status({ state: 'working', healing: 1, analysing: 2 }))).toBe('healing 1 · analysing 2');
    expect(chipText(status({ state: 'paused', reason: 'daily cap reached' }))).toBe('paused');
  });
});

describe('findingOutcome', () => {
  const f = (over: Partial<SentinelFindingView>): SentinelFindingView => ({
    id: 1,
    state: '',
    reason: '',
    output: '',
    kind: 'guidance',
    title: 't',
    text: 'x',
    evidence: '',
    created_at: '2026-11-13T10:00:00Z',
    ...over,
  });
  const PR = 'https://github.com/acme/app/pull/12';

  it.each<[Partial<SentinelFindingView>, string | undefined, boolean | undefined]>([
    [{ state: 'queued' }, undefined, undefined],
    [{ state: 'pending' }, undefined, undefined],
    [{ state: 'proposed', output: 'https://github.com/acme/app/pull/3' }, 'https://github.com/acme/app/pull/3', true],
    [{ state: 'proposed' }, PR, true],
    [{ state: 'merged' }, undefined, undefined],
    [{ state: 'rejected' }, undefined, undefined],
    [{ state: 'duplicate', reason: 'like finding 2' }, undefined, undefined],
    [{ state: 'dropped', reason: 'holds a secret' }, undefined, undefined],
    [{ state: 'carded', output: 'T-040' }, '/projects/p?card=T-040', undefined],
  ])('%j links to %s', (over, href, external) => {
    const got = findingOutcome(f(over), 'p', PR);
    expect(got).not.toBeNull();
    expect(got?.text).toBeTruthy();
    expect(got?.href).toBe(href);
    expect(got?.external).toBe(external);
  });

  it.each<Partial<SentinelFindingView>>([
    { state: 'queued', reason: 'over the daily limit' },
    { state: 'duplicate', reason: 'like finding 2' },
    { state: 'dropped', reason: 'holds a secret' },
  ])('%j shows its reason', (over) => {
    expect(findingOutcome(f(over), 'p', PR)?.text).toContain(over.reason);
  });

  it('names the friction card it became', () => {
    expect(findingOutcome(f({ state: 'carded', output: 'T-040' }), 'p', PR)?.text).toContain('T-040');
  });

  it.each(['note', 'held', ''] as const)('%j has no outcome', (state) => {
    expect(findingOutcome(f({ state }), 'p', PR)).toBeNull();
  });

  it('has no link for a proposed finding when no PR is known', () => {
    const got = findingOutcome(f({ state: 'proposed' }), 'p');
    expect(got).not.toBeNull();
    expect(got?.href).toBeUndefined();
  });
});

describe('pillTone', () => {
  it.each<[string, PillTone]>([
    ['healed', 'green'],
    ['merged', 'green'],
    ['analysed', 'green'],
    ['forward_passed', 'green'],
    ['parked', 'wait'],
    ['needs_a_human', 'wait'],
    ['changed_outside', 'wait'],
    ['rerun_failed', 'red'],
    ['forward_failed', 'red'],
    ['analyse_failed', 'red'],
    ['revision_failed', 'red'],
    ['revision_refused', 'red'],
    ['proposed', 'aqua'],
    ['open', 'aqua'],
    ['healing', 'aqua'],
    ['analysing', 'aqua'],
    ['investigate', 'aqua'],
    ['check', 'aqua'],
    ['apply', 'aqua'],
    ['usage_wait', 'aqua'],
    ['rerun', 'aqua'],
    ['forwarded', 'aqua'],
    ['analyse', 'aqua'],
    ['revision_stale', 'neutral'],
    ['analyse_skipped', 'neutral'],
    ['closed', 'neutral'],
    ['rejected', 'neutral'],
    ['queued', 'neutral'],
    ['pending', 'neutral'],
    ['carded', 'neutral'],
    ['duplicate', 'neutral'],
    ['dropped', 'neutral'],
    ['held', 'neutral'],
    ['note', 'neutral'],
    ['watching', 'neutral'],
    ['not_ours', 'neutral'],
    ['', 'neutral'],
  ])('%s -> %s', (state, want) => {
    expect(pillTone(state)).toBe(want);
  });
});

const run: SentinelRunView = {
  origin: 'mcp',
  autonomous: true,
  attempts: 1,
  max_attempts: 2,
  started_at: '2026-11-13T10:00:00Z',
  human_stop: false,
};

function heal(over: Partial<SentinelHealView> = {}): SentinelHealView {
  return {
    id: 1,
    kind: 'heal',
    cause: 'gave_up',
    stage: 'healed',
    reason: '',
    diagnosis: '',
    dropped: [],
    spend_usd: 0,
    created_at: '2026-11-13T10:10:00Z',
    updated_at: '2026-11-13T10:20:00Z',
    ...over,
  };
}

function view(over: Partial<SentinelCardView> = {}): SentinelCardView {
  return { run, heals: [], findings: [], analyses: [], standing_pr: null, guidance_revision: null, ...over };
}

const analysis = (stage: string, spend = 0) => ({ id: 2, run_id: 'r', stage, reason: '', spend_usd: spend, created_at: '2026-11-13T10:00:00Z' });

describe('cardStatus', () => {
  it.each<[string, Partial<SentinelCardView>, CardStatus]>([
    ['no run row', { run: null }, 'not_ours'],
    ['nothing open', {}, 'watching'],
    ['latest heal healed', { heals: [heal({ stage: 'healed' })] }, 'healed'],
    ['latest heal parked', { heals: [heal({ stage: 'parked' })] }, 'needs_a_human'],
    ['latest rerun failed', { heals: [heal({ stage: 'rerun_failed' })] }, 'needs_a_human'],
    ['a park the card was relaunched past', { run: { ...run, started_at: '2026-11-13T11:00:00Z' }, heals: [heal({ stage: 'parked' })] }, 'watching'],
    ['an open heal', { heals: [heal({ stage: 'investigate' })] }, 'healing'],
    ['a heal waiting on usage', { heals: [heal({ stage: 'usage_wait' })] }, 'healing'],
    ['an open analysis', { analyses: [analysis('analyse')] }, 'analysing'],
    ['an open heal before an open analysis', { heals: [heal({ stage: 'rerun' })], analyses: [analysis('analyse')] }, 'healing'],
    ['an open analysis after a park', { heals: [heal({ stage: 'parked' })], analyses: [analysis('analyse')] }, 'analysing'],
    ['an older park under a newer healed heal', { heals: [heal({ id: 2, stage: 'healed' }), heal({ stage: 'parked' })] }, 'healed'],
    ['a forward row is not a heal', { heals: [heal({ kind: 'forward', stage: 'forwarded', source_card: 'T-9' })] }, 'watching'],
    ['a closed heal', { heals: [heal({ stage: 'closed' })] }, 'watching'],
  ])('%s', (_, over, want) => {
    expect(cardStatus(view(over))).toBe(want);
  });
});

describe('sentinelSpend', () => {
  it('adds heals and analyses', () => {
    const v = view({ heals: [heal({ spend_usd: 0.42 }), heal({ id: 3, spend_usd: 1 })], analyses: [analysis('analysed', 0.31)] });
    expect(sentinelSpend(v)).toBeCloseTo(1.73);
  });

  it('is zero with nothing spent', () => {
    expect(sentinelSpend(view())).toBe(0);
  });
});

describe('splitFindings', () => {
  const f = (id: number, kind: SentinelFindingView['kind']): SentinelFindingView => ({
    id,
    kind,
    state: '',
    reason: '',
    output: '',
    title: '',
    text: '',
    evidence: '',
    created_at: '2026-11-13T10:00:00Z',
  });

  it('separates friction from notes, grouped by kind', () => {
    const got = splitFindings([
      f(1, 'hard_card'),
      f(2, 'code'),
      f(3, 'guidance'),
      f(4, 'project_verify'),
      f(5, 'ecosystem'),
      f(6, 'card_advice'),
      f(7, 'guidance'),
    ]);
    expect(got.friction.map((x) => x.id)).toEqual([3, 7, 2, 5]);
    expect(got.notes.map((x) => x.id)).toEqual([6, 4, 1]);
  });
});

describe('findingLabel', () => {
  const f = (title: string, text: string): SentinelFindingView => ({
    id: 1,
    kind: 'project_verify',
    state: '',
    reason: '',
    output: '',
    title,
    text,
    evidence: '',
    created_at: '2026-11-13T10:00:00Z',
  });

  it.each<[string, string, string]>([
    ['Add a hint', 'body', 'Add a hint'],
    ['', 'First line.\nSecond line.', 'First line.'],
    ['  ', '\n  Indented first line\n', 'Indented first line'],
    ['', '', 'project verify'],
    [' ', ' \n ', 'project verify'],
  ])('title %j, text %j -> %j', (title, text, want) => {
    expect(findingLabel(f(title, text))).toBe(want);
  });
});
