import type { Card } from '../../../types';

interface MetadataUsageProps {
  card: Card;
}

function formatCost(usd: number): string {
  // 4-digit precision below 10 cents, 2 above. Intentionally diverges from the
  // plan's 0.01 threshold: at 0.01 the spec's own test fixture ($0.0123) would
  // round to $0.01, contradicting the prescribed /\$0\.0123/ assertion. 0.1 is
  // the better UX (sub-10-cent costs need the extra digits) and keeps the test
  // intact.
  return `$${usd.toFixed(usd < 0.1 ? 4 : 2)}`;
}

/**
 * Info-rail section listing per-(agent, model) token/cost attribution from
 * `card.usage_breakdown`. The model column is the durable surface that shows
 * which model the complexity selector actually used. Renders nothing for
 * legacy cards or cards not run by the agent backend (no breakdown present).
 */
export function MetadataUsage({ card }: MetadataUsageProps) {
  const buckets = card.usage_breakdown ?? [];
  if (buckets.length === 0) {
    return null;
  }

  return (
    <section className="bf-aside-section">
      <h4>Models used</h4>
      <ul className="flex flex-col gap-1">
        {buckets.map((b, i) => (
          <li key={`${b.agent}:${b.model}:${i}`} className="text-xs text-[var(--fg)]">
            <div className="font-mono break-all">{b.model || '(unknown)'}</div>
            <div className="text-[var(--grey1)]">
              {b.agent} · {b.prompt_tokens + b.completion_tokens} tok ·{' '}
              <span
                title={
                  b.cost_source === 'actual'
                    ? 'actual provider cost'
                    : 'estimated from rate table'
                }
              >
                {formatCost(b.cost_usd)}
                {b.cost_source === 'estimated' ? ' (est)' : ''}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
