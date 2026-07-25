/**
 * Cost formatting shared by usage displays. 4-digit precision below 10 cents,
 * 2 above. Intentionally diverges from the plan's 0.01 threshold: at 0.01 the
 * spec's own test fixture ($0.0123) would round to $0.01, contradicting the
 * prescribed /\$0\.0123/ assertion. 0.1 is the better UX (sub-10-cent costs
 * need the extra digits) and keeps the test intact.
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
