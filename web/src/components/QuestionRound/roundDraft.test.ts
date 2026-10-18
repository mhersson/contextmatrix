import { describe, expect, it } from 'vitest';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound, type Round } from './parseRound';
import {
  EMPTY_DRAFT,
  SUMMARY_MAX_BYTES,
  answeredCount,
  buildReply,
  effectiveAnswer,
  hasUnsent,
  replySummary,
  storedAnswer,
  type RoundDraft,
} from './roundDraft';

const round = parseRound(rawRound({ answers: [{ number: 1, option: 1 }] }))!;
const q = (r: Round, n: number) => r.questions.find((x) => x.number === n)!;
const draft = (picks: RoundDraft['picks'], decisions: RoundDraft['decisions'] = {}): RoundDraft => ({ picks, decisions });

describe('round answers', () => {
  it('prefers a local pick over the stored answer', () => {
    const d = draft({ 1: { kind: 'option', option: 2, title: 'Helm' } });
    expect(effectiveAnswer(round, d, q(round, 1))).toEqual({ number: 1, option: 2 });
    expect(effectiveAnswer(round, EMPTY_DRAFT, q(round, 1))).toEqual({ number: 1, option: 1 });
  });

  it('drops an option pick once the option at that position changed', () => {
    const revised = parseRound(
      rawRound({
        questions: [
          {
            number: 3,
            title: 'Postgres',
            text: 'How should the service database run?',
            options: [{ title: 'CloudNativePG operator' }, { title: 'Managed database outside the cluster' }, { title: 'Plain StatefulSet' }],
          },
        ],
      }),
    )!;
    const d = draft({ 3: { kind: 'option', option: 2, title: 'Plain StatefulSet' } });
    expect(effectiveAnswer(revised, d, q(revised, 3))).toBeNull();
  });

  it('falls back to the stored answer when the own answer is blank, and trims a typed one', () => {
    expect(effectiveAnswer(round, draft({ 1: { kind: 'own', text: '   ' } }), q(round, 1))).toEqual({ number: 1, option: 1 });
    expect(effectiveAnswer(round, draft({ 2: { kind: 'own', text: ' Loki only ' } }), q(round, 2))).toEqual({ number: 2, text: 'Loki only' });
  });

  it('ignores a stored answer whose option is out of range', () => {
    const bad = parseRound(rawRound({ answers: [{ number: 2, option: 5 }] }))!;
    expect(storedAnswer(bad, q(bad, 2))).toBeNull();
    expect(answeredCount(bad, EMPTY_DRAFT)).toBe(0);
  });

  it('reports unsent changes only when a pick differs from what is stored', () => {
    expect(hasUnsent(round, draft({ 1: { kind: 'option', option: 1, title: 'Kustomize' } }))).toBe(false);
    expect(hasUnsent(round, draft({ 2: { kind: 'option', option: 1, title: 'Grafana + Loki + Fluent Bit' } }))).toBe(true);
    const withA = parseRound(rawRound({ assumptions: [{ id: 'A1', text: 'x' }], decisions: [{ id: 'A1', accept: true }] }))!;
    expect(hasUnsent(withA, draft({}, { A1: true }))).toBe(false);
    expect(hasUnsent(withA, draft({}, { A1: false }))).toBe(true);
  });

  it('builds a reply holding every stored and local answer; final only on submit', () => {
    const d = draft({ 2: { kind: 'own', text: ' Loki only ' } });
    expect(buildReply('round:1', round, d, false)).toEqual({
      prompt_id: 'round:1',
      answers: [{ number: 1, option: 1 }, { number: 2, text: 'Loki only' }],
      decisions: [],
    });
    expect(buildReply('round:1', round, d, true).final).toBe(true);
    expect(answeredCount(round, d)).toBe(2);
  });

  it('keeps the summary under the byte cap whatever the script', () => {
    const many = parseRound(
      rawRound({
        questions: Array.from({ length: 60 }, (_, i) => ({ number: i + 1, title: `T${i + 1}`, text: 'Q?', options: [{ title: 'A' }] })),
      }),
    )!;
    const own = 'ø'.repeat(5000);
    const d = draft(Object.fromEntries(many.questions.map((x) => [x.number, { kind: 'own' as const, text: own }])));
    const summary = replySummary(many, buildReply('round:1', many, d, true));
    expect(summary.length).toBeGreaterThan(0);
    expect(new TextEncoder().encode(summary).length).toBeLessThanOrEqual(SUMMARY_MAX_BYTES);
  });
});

describe('reply summary', () => {
  it('names an assumption by its text in the summary, never by its round-local id', () => {
    const withA = parseRound(rawRound({ assumptions: [{ id: 'A1', text: 'One cluster per environment.' }] }))!;
    const summary = replySummary(withA, buildReply('round:1', withA, draft({}, { A1: false }), false));
    expect(summary).toContain('One cluster per environment.');
    expect(summary).not.toMatch(/\bA1\b/);
  });
});
