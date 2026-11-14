import { describe, it, expect } from 'vitest';
import type { SentinelFindingView, SentinelState, SentinelStatus } from '../types';
import { chipText, findingOutcome, lineDiff, spendLine, verifyLines, stripText, stripTone, windowLines } from './sentinel';

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
