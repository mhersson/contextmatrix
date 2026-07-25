import { Fragment } from 'react';
import type { Card } from '../../../types';
import { formatCost, formatTokens } from '../../../lib/format';
import { groupBucketsByAgent } from '../utils';

interface MetadataUsageProps {
  card: Card;
}

/**
 * Info-rail section listing per-(agent, model) token/cost attribution from
 * `card.usage_breakdown` as a three-column grid (model | tokens | cost), one
 * agent header per group. The model column is the durable surface that shows
 * which model the complexity selector actually used. Renders nothing when
 * there is neither a breakdown nor subtask spend.
 */
export function MetadataUsage({ card }: MetadataUsageProps) {
  const buckets = card.usage_breakdown ?? [];
  const ownCost = card.token_usage?.estimated_cost_usd ?? 0;
  const subtaskCost = card.subtask_cost_usd ?? 0;
  const total = ownCost + subtaskCost;
  if (buckets.length === 0 && subtaskCost === 0) {
    return null;
  }

  return (
    <section className="bf-aside-section">
      <h4>Models used</h4>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-baseline gap-x-3 gap-y-1 text-[12px] text-[var(--fg)]">
        {total > 0 && (
          <>
            <span className="truncate">
              Total{subtaskCost > 0 ? ' incl. subtasks' : ''}
            </span>
            <span aria-hidden="true" />
            <span className="text-right tabular-nums">{formatCost(total)}</span>
            {subtaskCost > 0 && ownCost > 0 && (
              <span className="col-span-3 text-[var(--grey1)]">
                this card {formatCost(ownCost)} · subtasks {formatCost(subtaskCost)}
              </span>
            )}
          </>
        )}
        {groupBucketsByAgent(buckets).map((group) => (
          <Fragment key={group.agent}>
            <span
              className="col-span-3 mt-1 font-mono text-[11px] text-[var(--grey0)] truncate"
              title={group.agent}
            >
              {group.agent}
            </span>
            {group.buckets.map((b, i) => {
              const tokens = b.prompt_tokens + b.completion_tokens;
              return (
                <Fragment key={`${b.model}:${i}`}>
                  <span className="font-mono truncate" title={b.model || '(unknown)'}>
                    {b.model || '(unknown)'}
                  </span>
                  <span
                    className="text-right tabular-nums text-[var(--grey1)]"
                    title={`${tokens.toLocaleString()} tokens`}
                  >
                    {formatTokens(tokens)}
                  </span>
                  <span
                    className="text-right tabular-nums"
                    title={
                      b.cost_source === 'actual'
                        ? 'actual provider cost'
                        : 'estimated from rate table'
                    }
                  >
                    {formatCost(b.cost_usd)}
                    {b.cost_source === 'estimated' ? '*' : ''}
                  </span>
                </Fragment>
              );
            })}
          </Fragment>
        ))}
      </div>
    </section>
  );
}
