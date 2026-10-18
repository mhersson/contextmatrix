import type { RoundAnswer } from '../../types';
import { safeGetJSON, safeSetJSON, safeRemove } from '../../utils/safeStorage';
import type { Round } from './parseRound';
import { sameAnswer, storedAnswer, storedDecision, type Pick, type RoundDraft } from './roundDraft';

export const DRAFT_STORAGE_KEY = 'contextmatrix-round-drafts';
export const DRAFT_CAP = 50;

export interface DraftScope {
  sessionID: string;
  promptId: string;
}

/** What each unsent choice was made against: the stored value at that moment, or null when none was stored. */
export interface DraftBases {
  picks: Readonly<Record<number, RoundAnswer | null>>;
  decisions: Readonly<Record<string, boolean | null>>;
}

export interface DraftState {
  draft: RoundDraft;
  own: Readonly<Record<number, string>>;
  bases: DraftBases;
}

export const EMPTY_STATE: DraftState = { draft: { picks: {}, decisions: {} }, own: {}, bases: { picks: {}, decisions: {} } };

interface Entry {
  saved: number;
  picks: Record<string, { pick: Pick; base: RoundAnswer | null }>;
  decisions: Record<string, { accept: boolean; base: boolean | null }>;
  own: Record<string, string>;
}

const keyOf = (s: DraftScope) => `${s.sessionID}/${s.promptId}`;
const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

function readMap(): Record<string, unknown> {
  const raw = safeGetJSON<unknown>(DRAFT_STORAGE_KEY);
  return isObject(raw) ? raw : {};
}

function writeMap(map: Record<string, unknown>): void {
  if (Object.keys(map).length === 0) safeRemove(DRAFT_STORAGE_KEY);
  else safeSetJSON(DRAFT_STORAGE_KEY, map);
}

function savedOf(v: unknown): number {
  return isObject(v) && typeof v.saved === 'number' ? v.saved : 0;
}

/** The choices that still count against the newest frame; everything else is dropped. */
function valid(round: Round, state: DraftState): DraftState {
  const picks: Record<number, Pick> = {};
  const basesPicks: Record<number, RoundAnswer | null> = {};
  const own: Record<number, string> = {};
  for (const q of round.questions) {
    const pick = state.draft.picks[q.number];
    if (!pick || !(q.number in state.bases.picks)) continue;
    const answer: RoundAnswer | null =
      pick.kind === 'option'
        ? q.options[pick.option - 1]?.title === pick.title
          ? { number: q.number, option: pick.option }
          : null
        : pick.text.trim() !== ''
          ? { number: q.number, text: pick.text.trim() }
          : null;
    const stored = storedAnswer(round, q);
    if (answer === null || !sameAnswer(stored, state.bases.picks[q.number]) || sameAnswer(stored, answer)) continue;
    picks[q.number] = pick;
    basesPicks[q.number] = state.bases.picks[q.number];
    if (state.own[q.number] !== undefined) own[q.number] = state.own[q.number];
  }
  const decisions: Record<string, boolean> = {};
  const basesDecisions: Record<string, boolean | null> = {};
  for (const a of round.assumptions) {
    const accept = state.draft.decisions[a.id];
    if (accept === undefined || !(a.id in state.bases.decisions)) continue;
    const stored = storedDecision(round, a.id);
    if (stored !== state.bases.decisions[a.id] || stored === accept) continue;
    decisions[a.id] = accept;
    basesDecisions[a.id] = stored;
  }
  return { draft: { picks, decisions }, own, bases: { picks: basesPicks, decisions: basesDecisions } };
}

function toEntry(state: DraftState): Entry {
  const entry: Entry = { saved: Date.now(), picks: {}, decisions: {}, own: {} };
  for (const [n, pick] of Object.entries(state.draft.picks)) entry.picks[n] = { pick, base: state.bases.picks[Number(n)] ?? null };
  for (const [id, accept] of Object.entries(state.draft.decisions)) entry.decisions[id] = { accept, base: state.bases.decisions[id] ?? null };
  for (const [n, text] of Object.entries(state.own)) if (text !== '') entry.own[n] = text;
  return entry;
}

function parseAnswer(v: unknown): RoundAnswer | null | undefined {
  if (v === null) return null;
  if (!isObject(v) || typeof v.number !== 'number') return undefined;
  return v as unknown as RoundAnswer;
}

function parsePick(v: unknown): Pick | null {
  if (!isObject(v)) return null;
  if (v.kind === 'option' && typeof v.option === 'number' && typeof v.title === 'string') return { kind: 'option', option: v.option, title: v.title };
  if (v.kind === 'own' && typeof v.text === 'string') return { kind: 'own', text: v.text };
  return null;
}

/** A saved entry as state, or null when its shape is not ours. */
function fromEntry(raw: unknown): DraftState | null {
  if (!isObject(raw) || !isObject(raw.picks) || !isObject(raw.decisions) || !isObject(raw.own)) return null;
  const picks: Record<number, Pick> = {};
  const basesPicks: Record<number, RoundAnswer | null> = {};
  for (const [n, v] of Object.entries(raw.picks)) {
    const pick = isObject(v) ? parsePick(v.pick) : null;
    const base = isObject(v) ? parseAnswer(v.base) : undefined;
    if (pick && base !== undefined && Number.isInteger(Number(n))) {
      picks[Number(n)] = pick;
      basesPicks[Number(n)] = base;
    }
  }
  const decisions: Record<string, boolean> = {};
  const basesDecisions: Record<string, boolean | null> = {};
  for (const [id, v] of Object.entries(raw.decisions)) {
    if (isObject(v) && typeof v.accept === 'boolean' && (v.base === null || typeof v.base === 'boolean')) {
      decisions[id] = v.accept;
      basesDecisions[id] = v.base;
    }
  }
  const own: Record<number, string> = {};
  for (const [n, text] of Object.entries(raw.own)) if (typeof text === 'string' && Number.isInteger(Number(n))) own[Number(n)] = text;
  return { draft: { picks, decisions }, own, bases: { picks: basesPicks, decisions: basesDecisions } };
}

/** The saved draft for this round, filtered against the newest frame; empty when none is saved or storage is unusable. */
export function loadDraft(scope: DraftScope, round: Round): DraftState {
  const saved = fromEntry(readMap()[keyOf(scope)]);
  return saved ? valid(round, saved) : EMPTY_STATE;
}

/** Writes the part of the state that still counts; an empty draft removes the entry. */
export function saveDraft(scope: DraftScope, round: Round, state: DraftState): void {
  const kept = valid(round, state);
  const map = readMap();
  const key = keyOf(scope);
  const empty = Object.keys(kept.draft.picks).length === 0 && Object.keys(kept.draft.decisions).length === 0;
  if (empty) {
    if (!(key in map)) return;
    delete map[key];
  } else {
    map[key] = toEntry(kept);
    const keys = Object.keys(map);
    if (keys.length > DRAFT_CAP) {
      keys.sort((a, b) => savedOf(map[a]) - savedOf(map[b]));
      for (const k of keys.slice(0, keys.length - DRAFT_CAP)) delete map[k];
    }
  }
  writeMap(map);
}

export function clearDraft(scope: DraftScope): void {
  const map = readMap();
  if (!(keyOf(scope) in map)) return;
  delete map[keyOf(scope)];
  writeMap(map);
}

/** Drops this session's entries for every round but the waiting one. */
export function pruneSession(sessionID: string, keepPromptId: string | null): void {
  const map = readMap();
  const prefix = `${sessionID}/`;
  const keep = keepPromptId === null ? null : `${prefix}${keepPromptId}`;
  let changed = false;
  for (const k of Object.keys(map)) {
    if (k.startsWith(prefix) && k !== keep) {
      delete map[k];
      changed = true;
    }
  }
  if (changed) writeMap(map);
}
