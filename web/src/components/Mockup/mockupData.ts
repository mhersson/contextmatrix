import type { BlueprintView } from '../../types';
import type { MockupSettings } from './frameProtocol';

export interface MockupOption {
  value: string;
  label: string;
}

export type MockupSuggestion =
  | { id: string; title: string; why: string; kind: 'switch'; default: 'on' | 'off' }
  | { id: string; title: string; why: string; kind: 'choice'; default: string; options: MockupOption[] };

export interface MockupRevision {
  source: string;
  suggestions: MockupSuggestion[];
  settings: MockupSettings;
  chosen: MockupSettings | null;
}

const ID = /^[a-z][a-z0-9_]{0,31}$/;
const VALUE = /^[a-z0-9_-]{1,32}$/;
const MAX_SUGGESTIONS = 12;

const isRecord = (x: unknown): x is Record<string, unknown> => typeof x === 'object' && x !== null && !Array.isArray(x);

function parseOptions(raw: unknown): MockupOption[] | null {
  if (!Array.isArray(raw)) return null;
  const options: MockupOption[] = [];
  for (const o of raw) {
    if (!isRecord(o) || typeof o.value !== 'string' || !VALUE.test(o.value)) return null;
    if (options.some((seen) => seen.value === o.value)) return null;
    options.push({ value: o.value, label: typeof o.label === 'string' && o.label !== '' ? o.label : o.value });
  }
  return options.length >= 2 && options.length <= 4 ? options : null;
}

function parseSuggestion(raw: unknown, seen: Set<string>): MockupSuggestion | null {
  if (!isRecord(raw) || typeof raw.id !== 'string' || !ID.test(raw.id) || seen.has(raw.id)) return null;
  if (typeof raw.title !== 'string' || raw.title.trim() === '') return null;
  const base = { id: raw.id, title: raw.title, why: typeof raw.why === 'string' ? raw.why : '' };
  if (raw.kind === 'switch') return { ...base, kind: 'switch', default: raw.default === 'on' ? 'on' : 'off' };
  if (raw.kind !== 'choice') return null;
  const options = parseOptions(raw.options);
  if (!options) return null;
  const fallback = options.some((o) => o.value === raw.default) ? (raw.default as string) : options[0].value;
  return { ...base, kind: 'choice', default: fallback, options };
}

function valid(s: MockupSuggestion, value: unknown): value is string {
  if (typeof value !== 'string') return false;
  return s.kind === 'switch' ? value === 'on' || value === 'off' : s.options.some((o) => o.value === value);
}

function normalise(raw: unknown, suggestions: readonly MockupSuggestion[]): MockupSettings {
  const given = isRecord(raw) ? raw : {};
  const out: Record<string, string> = {};
  for (const s of suggestions) {
    const v = Object.hasOwn(given, s.id) ? given[s.id] : undefined;
    out[s.id] = valid(s, v) ? v : s.default;
  }
  return out;
}

/** Reads a mockup revision's data; what is invalid is dropped and a missing source gives null. */
export function parseMockup(data: unknown): MockupRevision | null {
  if (!isRecord(data) || typeof data.source !== 'string' || data.source.trim() === '') return null;
  const suggestions: MockupSuggestion[] = [];
  const seen = new Set<string>();
  const rawList = Array.isArray(data.suggestions) ? data.suggestions : [];
  for (const raw of rawList) {
    const s = parseSuggestion(raw, seen);
    if (!s) continue;
    seen.add(s.id);
    suggestions.push(s);
    if (suggestions.length === MAX_SUGGESTIONS) break;
  }
  return {
    source: data.source,
    suggestions,
    settings: normalise(data.settings, suggestions),
    chosen: isRecord(data.chosen) ? normalise(data.chosen, suggestions) : null,
  };
}

/** The mockup is decided: a revision was used or the mockups were skipped. */
export function mockupDecided(view: BlueprintView): boolean {
  return view.artifacts.some((a) => a.stage === 'mockup' && (a.status === 'approved' || a.status === 'rejected'));
}
