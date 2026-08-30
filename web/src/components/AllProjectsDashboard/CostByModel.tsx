import { useMemo } from 'react';
import type { ModelCost } from '../../types';
import { chipTint } from '../../lib/chip';
import { DeckPanel } from './DeckPanel';

const ATTRIBUTION_NOTE =
  'Each card is attributed to its most-recently-used model. Cards that used multiple models show under the last one.';

export function CostByModel({ modelCosts }: { modelCosts: ModelCost[] }) {
  const sorted = useMemo(
    () => [...modelCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd),
    [modelCosts],
  );
  const max = sorted.reduce(
    (acc, m) => (m.estimated_cost_usd > acc ? m.estimated_cost_usd : acc),
    0,
  );

  return (
    <DeckPanel
      area="models"
      accent="var(--green)"
      title="Cost by model"
      meta={
        <>
          <span>{modelCosts.length} models · 30d</span>
          <span
            role="note"
            tabIndex={0}
            aria-label={ATTRIBUTION_NOTE}
            title={ATTRIBUTION_NOTE}
            style={{
              color: 'var(--grey1)',
              cursor: 'help',
              lineHeight: 1,
            }}
          >
            <span aria-hidden="true">&#9432;</span>
          </span>
        </>
      }
    >
      {sorted.length === 0 ? (
        <div className="apd-panel-empty" style={{ flex: 1 }}>
          No cost in the last 30 days
        </div>
      ) : (
        <div className="apd-panel-body" style={{ padding: '0 14px 10px' }}>
          {sorted.map((mc) => {
            const pct = max > 0 ? Math.max(2, (mc.estimated_cost_usd / max) * 100) : 0;
            return (
              <div
                key={mc.model}
                className="apd-cost-row"
                style={{ borderBottom: '1px solid var(--bg1)' }}
              >
                <span
                  className="chip-pill apd-model-pill"
                  style={chipTint('var(--aqua)')}
                  title={mc.model}
                >
                  {mc.model}
                </span>
                <div className="apd-bar-wrap" style={{ backgroundColor: 'var(--bg1)' }}>
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
                    fontSize: 11.5,
                    color: 'var(--yellow)',
                    fontVariantNumeric: 'tabular-nums',
                    letterSpacing: '-0.01em',
                  }}
                  title={mc.has_estimates ? 'includes costs estimated from the rate table' : undefined}
                >
                  ${mc.estimated_cost_usd.toFixed(2)}
                  {mc.has_estimates ? '*' : ''}
                </span>
                <span
                  style={{
                    textAlign: 'right',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 10.5,
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
      )}
    </DeckPanel>
  );
}
