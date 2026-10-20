import { describe, it, expect } from 'vitest';
import { MOCKUP_V2, ref, viewAtMockup } from '../../test-utils/blueprintFixtures';
import { mockupDecided, parseMockup } from './mockupData';

const base = MOCKUP_V2.data as Record<string, unknown>;
const sug = (extra: Record<string, unknown>) => ({ id: 'a', title: 'A', kind: 'switch', why: 'w', default: 'off', ...extra });
const choice = (options: unknown, extra: Record<string, unknown> = {}) =>
  sug({ kind: 'choice', default: 'x', options, ...extra });
const opt = (value: unknown) => ({ value, label: String(value) });
const ids = (data: unknown) => parseMockup(data)?.suggestions.map((s) => s.id);
const withSuggestions = (suggestions: unknown[], more: Record<string, unknown> = {}) => ({
  source: '<p>x</p>',
  suggestions,
  ...more,
});

describe('parseMockup', () => {
  it('reads the fixture exactly', () => {
    const m = parseMockup(base)!;
    expect(m.source).toBe(base.source);
    expect(m.suggestions.map((s) => [s.id, s.kind, s.default])).toEqual([
      ['layout', 'choice', 'rows'],
      ['group', 'switch', 'off'],
      ['tick', 'choice', 'strike'],
    ]);
    expect(m.settings).toEqual({ layout: 'rows', group: 'off', tick: 'strike' });
    expect(m.chosen).toBeNull();
  });

  it.each([
    ['no source', {}],
    ['a blank source', { source: '  \n' }],
    ['a non-string source', { source: 4 }],
    ['non-object data', 'x'],
    ['null data', null],
  ])('gives null for %s', (_n, data) => {
    expect(parseMockup(data)).toBeNull();
  });

  it.each([
    ['a bad id', sug({ id: 'Bad-Id' })],
    ['an unknown kind', sug({ kind: 'slider' })],
    ['no title', sug({ title: undefined })],
    ['a choice with one option', choice([opt('x')])],
    ['a choice with five options', choice(['a', 'b', 'c', 'd', 'e'].map(opt))],
    ['a duplicate option value', choice([opt('x'), opt('x')])],
    ['a malformed option value', choice([opt('x'), opt('Bad Value')])],
  ])('drops a suggestion with %s', (_n, bad) => {
    expect(ids(withSuggestions([bad, sug({ id: 'ok' })]))).toEqual(['ok']);
  });

  it('drops a suggestion whose id was already seen', () => {
    const m = parseMockup(withSuggestions([sug({ title: 'First' }), sug({ title: 'Second' })]))!;
    expect(m.suggestions.map((s) => s.title)).toEqual(['First']);
  });

  it('defaults why to empty and bad defaults to a valid value', () => {
    const m = parseMockup(
      withSuggestions([
        sug({ id: 'sw', why: undefined, default: 'maybe' }),
        choice([opt('p'), opt('q')], { id: 'ch', default: 'zzz' }),
      ]),
    )!;
    expect(m.suggestions[0]).toMatchObject({ id: 'sw', why: '', default: 'off' });
    expect(m.suggestions[1]).toMatchObject({ id: 'ch', default: 'p' });
  });

  it('normalises settings and chosen against the suggestions', () => {
    const m = parseMockup(
      withSuggestions([sug({ id: 'sw', default: 'on' }), choice([opt('p'), opt('q')], { id: 'ch', default: 'p' })], {
        settings: { ch: 'nope', ghost: 'x' },
        chosen: { sw: 'off', ch: 'q', ghost: 'x' },
      }),
    )!;
    expect(m.settings).toEqual({ sw: 'on', ch: 'p' });
    expect(m.chosen).toEqual({ sw: 'off', ch: 'q' });
  });

  it('keeps the first 12 suggestions', () => {
    const many = Array.from({ length: 15 }, (_, i) => sug({ id: `s${i}` }));
    expect(ids(withSuggestions(many))).toEqual(many.slice(0, 12).map((s) => s.id));
  });

  it('survives suggestions that are not a list', () => {
    expect(parseMockup({ source: 'x', suggestions: 'no', settings: 3 })).toMatchObject({ suggestions: [], settings: {} });
  });
});

describe('mockupDecided', () => {
  it.each([
    ['approved', true],
    ['rejected', true],
    ['current', false],
    ['superseded', false],
  ] as const)('a mockup ref %s gives %s', (status, decided) => {
    const view = viewAtMockup();
    view.artifacts = [...view.artifacts.filter((a) => a.version !== 2), ref('mockup', 2, status)];
    expect(mockupDecided(view)).toBe(decided);
  });
});
