import type { SentinelStatus, SentinelVerify } from '../types';

export type SentinelTone = 'active' | 'paused' | 'stopped';

/** The strip's right-hand status text. */
export function stripText(s: SentinelStatus): string {
  switch (s.state) {
    case 'working':
      return `healing ${s.healing}`;
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

/** Line diff by longest common subsequence. Empty text has no lines. */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before === '' ? [] : before.split('\n');
  const b = after === '' ? [] : after.split('\n');
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
