import type { AboutComponent, AboutComponentName, AboutResponse, BuildChange } from '../types';

export const ABOUT_REFRESH_MS = 15 * 60_000;
export const ABOUT_SEEN_KEY = 'contextmatrix-about-seen';

export interface SeenMark {
  number: number;
  merged_at: string;
}
export type SeenMap = Partial<Record<AboutComponentName, SeenMark>>;
export type UnseenCounts = Partial<Record<AboutComponentName, number>>;

const LABELS: Record<AboutComponentName, string> = {
  contextmatrix: 'ContextMatrix',
  agent: 'Agent',
  chat: 'Chat',
  architect: 'Architect',
};
const NAMES = Object.keys(LABELS) as AboutComponentName[];

export function componentLabel(name: AboutComponentName): string {
  return LABELS[name];
}

export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

export function changesOf(c: AboutComponent): BuildChange[] {
  return c.build?.changes ?? [];
}

export function changeKind(kind: string): 'feat' | 'fix' | 'change' {
  return kind === 'feat' || kind === 'fix' ? kind : 'change';
}

export function versionLabel(c: AboutComponent): string {
  if (c.status === 'unreachable') return '-';
  if (!c.build) return 'version unknown';
  if (c.build.version) return c.build.version;
  if (c.build.commit) return shortSha(c.build.commit);
  return 'unknown build';
}

/** True when the running binary and its worker image come from different commits. */
export function imageMismatch(c: AboutComponent): boolean {
  const commit = c.build?.commit;
  const image = c.worker_image_revision;
  return !!commit && !!image && commit !== image;
}

function parseDate(iso: string | undefined): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

function shortDate(d: Date): string {
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function formatBuilt(iso: string | undefined): string {
  const d = parseDate(iso);
  if (!d) return '-';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${shortDate(d)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function formatMergedDate(iso: string): string {
  const d = parseDate(iso);
  return d ? shortDate(d) : '';
}

/** Changes (newest first) newer than the seen mark. merged_at is UTC RFC 3339, so string order is merge order. */
export function newCount(changes: BuildChange[], mark: SeenMark | undefined): number {
  if (!mark) return changes.length;
  const at = changes.findIndex((c) => c.number === mark.number);
  if (at >= 0) return at;
  if (!mark.merged_at) return 0;
  let n = 0;
  for (const c of changes) {
    if (!c.merged_at || c.merged_at <= mark.merged_at) break;
    n++;
  }
  return n;
}

export function unseenCounts(about: AboutResponse | null, seen: SeenMap): UnseenCounts {
  const out: UnseenCounts = {};
  for (const c of about?.components ?? []) {
    const n = newCount(changesOf(c), seen[c.name]);
    if (n > 0) out[c.name] = n;
  }
  return out;
}

/** Marks each component's newest change as seen; components without changes keep their old mark. */
export function nextSeen(prev: SeenMap, components: AboutComponent[]): SeenMap {
  const next: SeenMap = { ...prev };
  for (const c of components) {
    const newest = changesOf(c)[0];
    if (newest) next[c.name] = { number: newest.number, merged_at: newest.merged_at };
  }
  return next;
}

export function parseSeen(raw: unknown): SeenMap {
  const out: SeenMap = {};
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return out;
  const rec = raw as Record<string, unknown>;
  for (const name of NAMES) {
    const m = rec[name];
    if (typeof m !== 'object' || m === null) continue;
    const { number, merged_at } = m as Record<string, unknown>;
    if (typeof number === 'number' && typeof merged_at === 'string') out[name] = { number, merged_at };
  }
  return out;
}
