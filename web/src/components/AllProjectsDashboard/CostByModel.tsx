import { useMemo } from 'react';
import type { ModelCost } from '../../types';

const TOP_MODEL_COSTS = 5;

export function CostByModel({ modelCosts }: { modelCosts: ModelCost[] }) {
  const sorted = useMemo(
    () => [...modelCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd),
    [modelCosts],
  );
  const top = sorted.slice(0, TOP_MODEL_COSTS);
  const max = top.reduce(
    (acc, m) => (m.estimated_cost_usd > acc ? m.estimated_cost_usd : acc),
    0,
  );

  if (top.length === 0) {
    return (
      <div
        style={{
          padding: '32px 20px',
          textAlign: 'center',
          fontSize: 12.5,
          color: 'var(--grey0)',
          fontStyle: 'italic',
        }}
      >
        No cost reported yet
      </div>
    );
  }

  return (
    <div style={{ padding: '14px 20px 18px' }}>
      {top.map((mc) => {
        const pct = max > 0 ? Math.max(2, (mc.estimated_cost_usd / max) * 100) : 0;
        return (
          <div
            key={mc.model}
            className="apd-cost-row"
            style={{ borderBottom: '1px solid var(--bg2)' }}
          >
            <span
              className="truncate"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 11.5,
                color: 'var(--aqua)',
                letterSpacing: '-0.01em',
              }}
              title={mc.model}
            >
              {mc.model}
            </span>
            <div className="apd-bar-wrap" style={{ backgroundColor: 'var(--bg2)' }}>
              <div
                className="apd-bar"
                style={{
                  width: `${pct}%`,
                  backgroundColor: 'var(--aqua)',
                }}
              />
            </div>
            <span
              style={{
                textAlign: 'right',
                fontFamily: 'var(--font-mono)',
                fontSize: 12,
                color: 'var(--yellow)',
                fontVariantNumeric: 'tabular-nums',
                letterSpacing: '-0.01em',
              }}
            >
              ${mc.estimated_cost_usd.toFixed(2)}
            </span>
            <span
              style={{
                textAlign: 'right',
                fontFamily: 'var(--font-mono)',
                fontSize: 11,
                color: 'var(--grey1)',
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              {mc.card_count}c
            </span>
          </div>
        );
      })}
    </div>
  );
}
