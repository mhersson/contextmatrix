import type { AssumptionDecision, RoundAnswer } from '../../types';

export interface RoundSource {
  tag: string;
  title: string;
  /** An http(s) URL, else '' (a code source, or a scheme that must never become a link). */
  url: string;
}

export interface RoundOption {
  title: string;
  summary: string;
  pros: string[];
  cons: string[];
  sources: RoundSource[];
}

export interface RoundQuestion {
  /** The blueprint-wide number, shown as Q<n>. */
  number: number;
  /** The tab label. */
  title: string;
  text: string;
  why: string;
  options: RoundOption[];
  /** 1-based position of the recommended option; 0 for none. */
  recommended: number;
}

export interface RoundAssumption {
  id: string;
  text: string;
}

export interface RoundChatMessage {
  /** "user" or the asking persona's id. */
  from: string;
  text: string;
}

/** A question round as the architect last published it, every list present. */
export interface Round {
  persona: string;
  stage: string;
  questions: RoundQuestion[];
  assumptions: RoundAssumption[];
  /** What the architect has stored so far. */
  answers: RoundAnswer[];
  decisions: AssumptionDecision[];
  chat: RoundChatMessage[];
  researchOff: boolean;
  responder: boolean;
  closed: boolean;
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const lines = (v: unknown): string[] => list(v).filter((s): s is string => typeof s === 'string' && s.trim() !== '');
const positive = (v: unknown): number => (typeof v === 'number' && Number.isInteger(v) && v > 0 ? v : 0);
const httpURL = (u: string): string => (/^https?:\/\//i.test(u) ? u : '');

// A malformed option keeps its slot: answers address options by position.
function parseOption(v: unknown, i: number): RoundOption {
  const o = isRecord(v) ? v : {};
  return {
    title: str(o.title).trim() || `Option ${i + 1}`,
    summary: str(o.summary),
    pros: lines(o.pros),
    cons: lines(o.cons),
    sources: list(o.sources).flatMap((s): RoundSource[] =>
      isRecord(s) && str(s.title).trim() !== '' ? [{ tag: str(s.tag), title: str(s.title), url: httpURL(str(s.url)) }] : [],
    ),
  };
}

function parseQuestion(v: unknown): RoundQuestion | null {
  if (!isRecord(v)) return null;
  const number = positive(v.number);
  if (number === 0 || str(v.text).trim() === '') return null;
  const options = list(v.options).map(parseOption);
  const recommended = positive(v.recommended);
  return {
    number,
    title: str(v.title).trim() || `Q${number}`,
    text: str(v.text),
    why: str(v.why),
    options,
    recommended: recommended <= options.length ? recommended : 0,
  };
}

function parseAnswer(v: unknown): RoundAnswer[] {
  if (!isRecord(v) || positive(v.number) === 0) return [];
  const option = positive(v.option);
  if (option > 0) return [{ number: positive(v.number), option }];
  const text = str(v.text);
  return text.trim() === '' ? [] : [{ number: positive(v.number), text }];
}

/** Reads a round body defensively; null when it is not an object or has nothing to answer. */
export function parseRound(raw: unknown): Round | null {
  if (!isRecord(raw)) return null;
  const numbers = new Set<number>();
  const questions = list(raw.questions).flatMap((v): RoundQuestion[] => {
    const q = parseQuestion(v);
    if (!q || numbers.has(q.number)) return [];
    numbers.add(q.number);
    return [q];
  });
  const ids = new Set<string>();
  const assumptions = list(raw.assumptions).flatMap((v): RoundAssumption[] => {
    if (!isRecord(v) || str(v.id) === '' || str(v.text).trim() === '' || ids.has(str(v.id))) return [];
    ids.add(str(v.id));
    return [{ id: str(v.id), text: str(v.text) }];
  });
  if (questions.length === 0 && assumptions.length === 0) return null;
  return {
    persona: str(raw.persona),
    stage: str(raw.stage),
    questions,
    assumptions,
    answers: list(raw.answers).flatMap(parseAnswer),
    decisions: list(raw.decisions).flatMap((v): AssumptionDecision[] =>
      isRecord(v) && str(v.id) !== '' && typeof v.accept === 'boolean' ? [{ id: str(v.id), accept: v.accept }] : [],
    ),
    chat: list(raw.chat).flatMap((v) => (isRecord(v) && str(v.text).trim() !== '' ? [{ from: str(v.from), text: str(v.text) }] : [])),
    researchOff: raw.research_off === true,
    responder: raw.responder === true,
    closed: raw.closed === true,
  };
}
