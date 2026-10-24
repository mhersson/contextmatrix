import type { AssumptionDecision, RoundAnswer, RoundReply } from '../../types';
import type { Round, RoundQuestion } from './parseRound';

/** An unsent choice. An option pick keeps the option's title, so a revised option list drops it instead of sending another option. */
export type Pick = { kind: 'option'; option: number; title: string } | { kind: 'own'; text: string };

/** Choices made in this pane and not sent yet; the round's own answers and decisions are what the architect stored. */
export interface RoundDraft {
  picks: Readonly<Record<number, Pick>>;
  decisions: Readonly<Record<string, boolean>>;
}

export const EMPTY_DRAFT: RoundDraft = { picks: {}, decisions: {} };

/** The summary's clip in UTF-8 bytes, well inside the blueprint message limit its content counts against. */
export const SUMMARY_MAX_BYTES = 8000;
const SUMMARY_ANSWER_CHARS = 200;

export function storedAnswer(round: Round, q: RoundQuestion): RoundAnswer | null {
  const a = round.answers.find((x) => x.number === q.number);
  if (!a) return null;
  if (a.option !== undefined && a.option > 0) return a.option <= q.options.length ? { number: q.number, option: a.option } : null;
  const text = (a.text ?? '').trim();
  return text === '' ? null : { number: q.number, text };
}

export function storedDecision(round: Round, id: string): boolean | null {
  return round.decisions.find((d) => d.id === id)?.accept ?? null;
}

export function effectiveAnswer(round: Round, draft: RoundDraft, q: RoundQuestion): RoundAnswer | null {
  const pick = draft.picks[q.number];
  if (pick?.kind === 'option' && q.options[pick.option - 1]?.title === pick.title) return { number: q.number, option: pick.option };
  if (pick?.kind === 'own' && pick.text.trim() !== '') return { number: q.number, text: pick.text.trim() };
  return storedAnswer(round, q);
}

export function effectiveDecision(round: Round, draft: RoundDraft, id: string): boolean | null {
  return draft.decisions[id] ?? storedDecision(round, id);
}

export function answerLabel(q: RoundQuestion, a: RoundAnswer): string {
  return a.option ? (q.options[a.option - 1]?.title ?? '') : (a.text ?? '');
}

/** Every question and every assumption. */
export function itemCount(round: Round): number {
  return round.questions.length + round.assumptions.length;
}

export function answeredCount(round: Round, draft: RoundDraft): number {
  return (
    round.questions.filter((q) => effectiveAnswer(round, draft, q) !== null).length +
    round.assumptions.filter((a) => effectiveDecision(round, draft, a.id) !== null).length
  );
}

export function sameAnswer(a: RoundAnswer | null, b: RoundAnswer | null): boolean {
  if (a === null || b === null) return a === b;
  return (a.option ?? 0) === (b.option ?? 0) && (a.text ?? '') === (b.text ?? '');
}

export function hasUnsent(round: Round, draft: RoundDraft): boolean {
  return (
    round.questions.some((q) => !sameAnswer(effectiveAnswer(round, draft, q), storedAnswer(round, q))) ||
    round.assumptions.some((a) => effectiveDecision(round, draft, a.id) !== storedDecision(round, a.id))
  );
}

/** Carries every answer and decision known now, stored or local, so a send can also change an earlier answer. */
export function buildReply(promptId: string, round: Round, draft: RoundDraft, final: boolean): RoundReply {
  const answers = round.questions.flatMap((q) => {
    const a = effectiveAnswer(round, draft, q);
    return a ? [a] : [];
  });
  const decisions = round.assumptions.flatMap((a): AssumptionDecision[] => {
    const d = effectiveDecision(round, draft, a.id);
    return d === null ? [] : [{ id: a.id, accept: d }];
  });
  return { prompt_id: promptId, answers, decisions, ...(final ? { final: true } : {}) };
}

const encoder = new TextEncoder();

function clipChars(s: string, max: number): string {
  return s.length <= max ? s : `${s.slice(0, max - 1)}…`;
}

function clipBytes(s: string, max: number): string {
  if (encoder.encode(s).length <= max) return s;
  let out = '';
  let bytes = 0;
  for (const ch of s) {
    const n = encoder.encode(ch).length;
    if (bytes + n > max - 3) break;
    out += ch;
    bytes += n;
  }
  return `${out}…`;
}

/** The readable summary stored with a reply; the architect reads the structured reply. */
export function replySummary(round: Round, reply: RoundReply): string {
  const total = itemCount(round);
  const sent = (reply.answers?.length ?? 0) + (reply.decisions?.length ?? 0);
  const out = [reply.final ? `Submitted all ${total} answers.` : `Sent ${sent} of ${total} answers; the rest later.`];
  for (const a of reply.answers ?? []) {
    const q = round.questions.find((x) => x.number === a.number);
    if (q) out.push(`Q${q.number} ${q.title}: ${clipChars(answerLabel(q, a), SUMMARY_ANSWER_CHARS)}`);
  }
  for (const d of reply.decisions ?? []) {
    const a = round.assumptions.find((x) => x.id === d.id);
    if (a) out.push(`${d.accept ? 'Accepted' : 'Turn into a question'}: ${clipChars(a.text, SUMMARY_ANSWER_CHARS)}`);
  }
  return clipBytes(out.join('\n'), SUMMARY_MAX_BYTES);
}
