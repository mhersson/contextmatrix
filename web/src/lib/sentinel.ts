import type { SentinelFindingView, SentinelStatus, SentinelVerify } from '../types';

export type SentinelTone = 'active' | 'paused' | 'stopped';

function workingText(s: SentinelStatus): string {
  if (s.analysing <= 0) return `healing ${s.healing}`;
  return s.healing > 0 ? `healing ${s.healing} · analysing ${s.analysing}` : `analysing ${s.analysing}`;
}

/** Full state text: the tooltip and accessible name of the chip, and the popover's state. */
export function stripText(s: SentinelStatus): string {
  switch (s.state) {
    case 'working':
      return workingText(s);
    case 'paused':
      return s.reason ?? 'paused';
    case 'not_running':
      return 'not running';
    case 'off':
      return 'off';
    default:
      return 'watching';
  }
}

/** The chip's state word: the pause reason is too long for the row, so it stays in the tooltip. */
export function chipText(s: SentinelStatus): string {
  return s.state === 'paused' ? 'paused' : stripText(s);
}

export function stripTone(s: SentinelStatus): SentinelTone {
  if (s.state === 'paused') return 'paused';
  if (s.state === 'not_running') return 'stopped';
  return 'active';
}

/** Endpoint channel: today's spend against the daily cap. */
export function spendLine(s: SentinelStatus): string {
  return `$${s.spent_today_usd.toFixed(2)} of $${s.daily_cap_usd.toFixed(2)} today`;
}

/** Claude Code channel: one line per window against the headroom threshold. */
export function windowLines(s: SentinelStatus): string[] {
  return (s.windows ?? []).map(
    (w) => `${w.window} ${Math.round(w.utilization * 100)}% of ${Math.round(s.headroom * 100)}%`,
  );
}

export interface DiffLine {
  kind: 'same' | 'add' | 'del';
  text: string;
}

/** Lines of text with leading and trailing blank lines dropped; a section body carries both. */
function diffLines(text: string): string[] {
  const lines = text.split('\n');
  let start = 0;
  let end = lines.length;
  while (start < end && lines[start].trim() === '') start++;
  while (end > start && lines[end - 1].trim() === '') end--;
  return lines.slice(start, end);
}

/** Line diff by longest common subsequence. Blank edge lines and empty text have no lines. */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = diffLines(before);
  const b = diffLines(after);
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      out.push({ kind: 'same', text: a[i] });
      i++;
      j++;
    } else if (i < a.length && (j === b.length || lcs[i + 1][j] >= lcs[i][j + 1])) {
      out.push({ kind: 'del', text: a[i++] });
    } else {
      out.push({ kind: 'add', text: b[j++] });
    }
  }
  return out;
}

/** A verify config as the lines a reader compares; absent is empty. */
export function verifyLines(v?: SentinelVerify): string {
  if (!v) return '';
  const lines = [`command: ${v.command ?? ''}`];
  if (v.timeout_seconds) lines.push(`timeout_seconds: ${v.timeout_seconds}`);
  if (v.env?.length) lines.push(`env: ${v.env.join(', ')}`);
  return lines.join('\n');
}

export interface FindingOutcome {
  text: string;
  href?: string;
  /** The href leaves the app and opens in a new tab. */
  external?: boolean;
}

/** Where a finding stands on its way to an output; null when nothing is worth saying. */
export function findingOutcome(f: SentinelFindingView, project: string, prURL?: string): FindingOutcome | null {
  switch (f.state) {
    case 'queued':
      return { text: f.reason || 'Waiting to be routed' };
    case 'pending':
      return { text: 'Waiting for the next guidance PR' };
    case 'proposed': {
      const href = f.output || prURL;
      return href ? { text: 'In the standing PR', href, external: true } : { text: 'In the standing PR' };
    }
    case 'merged':
      return { text: 'Merged' };
    case 'rejected':
      return { text: 'Rejected in review' };
    case 'duplicate':
      return { text: f.reason };
    case 'dropped':
      return { text: `Dropped: ${f.reason}` };
    case 'carded':
      return { text: `Friction card ${f.output}`, href: `/projects/${project}?card=${f.output}` };
    default:
      return null;
  }
}
