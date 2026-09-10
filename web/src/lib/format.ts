/**
 * Cost formatting shared by usage displays. 4-digit precision below 10 cents,
 * 2 above: sub-10-cent LLM costs would otherwise all round to $0.01 or $0.00
 * and become indistinguishable.
 */
export function formatCost(usd: number): string {
  return `$${usd.toFixed(usd < 0.1 ? 4 : 2)}`;
}

/** Compact token count: 583000 -> "583k", 1234567 -> "1.2M", 1500 -> "1.5k". */
export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 10_000) return `${Math.round(n / 1_000)}k`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** Coarse "how long ago" for an RFC 3339 stamp: "just now", "5 min ago", "3 h ago", "2 d ago". */
export function formatRelativeTime(iso: string, now: number = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return 'unknown';
  const s = Math.max(0, Math.round((now - then) / 1000));
  if (s < 60) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}
