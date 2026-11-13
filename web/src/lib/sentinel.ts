import type { SentinelStatus } from '../types';

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
