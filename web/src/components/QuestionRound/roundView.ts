import { personaName } from '../Blueprint/stages';
import type { Round } from './parseRound';

/** open takes answers; paused shows the round with sending disabled (session not live); readonly is a responder round. */
export type RoundMode = 'open' | 'paused' | 'readonly';

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;

export function countText(round: Round, joiner = ' and '): string {
  const q = round.questions.length;
  const a = round.assumptions.length;
  if (q === 0) return plural(a, 'assumption');
  return a === 0 ? plural(q, 'question') : `${plural(q, 'question')}${joiner}${plural(a, 'assumption')}`;
}

/** The asking persona inside a sentence: "the system designer". */
export function personaPhrase(persona: string): string {
  return persona ? `the ${personaName(persona).toLowerCase()}` : 'the architect';
}
