import type { CSSProperties, ReactNode } from 'react';
import { Sparkline } from '../Sparkline/Sparkline';

interface KpiRowProps {
  costLast30dUsd: number;
  costPrior30dUsd: number;
  costSeries30d: number[] | undefined;
  costHasEstimates?: boolean;
  stateCountsParents: Record<string, number>;
  doneTodayParents: number;
  chatCostLast30dUsd: number;
  chatCostPrior30dUsd: number;
  chatCostSeries30d: number[] | undefined;
}

interface KpiTileProps {
  label: string;
  badge: string;
  value: ReactNode;
  accent: 'blue' | 'yellow' | 'green' | 'aqua';
  /** Cost tiles keep a neutral figure; the accent only drives border/wash. */
  neutralValue?: boolean;
  tooltip?: string;
  delta?: { pct: number; up: boolean };
  sparkline?: { values: number[]; color: string };
}

const ACCENT_TO_VAR: Record<KpiTileProps['accent'], string> = {
  blue: 'var(--blue)',
  yellow: 'var(--yellow)',
  green: 'var(--green)',
  aqua: 'var(--aqua)',
};

function KpiTile({ label, badge, value, accent, neutralValue, tooltip, delta, sparkline }: KpiTileProps) {
  const accentVar = ACCENT_TO_VAR[accent];
  return (
    <div
      className="apd-kpi"
      title={tooltip}
      // aria-label intentionally not set: accessible name is composed from descendants
      style={{ '--apd-acc': accentVar } as CSSProperties}
    >
      <div className="apd-kpi-label">
        <span>{label}</span>
        <span className="apd-kpi-badge">{badge}</span>
      </div>
      <div className="apd-kpi-value-row">
        <span className="apd-kpi-value" style={{ color: neutralValue ? 'var(--fg)' : accentVar }}>
          {value}
          {delta !== undefined && (
            <span
              className={`apd-kpi-delta metric-tile__delta ${delta.up ? 'metric-tile__delta--up' : 'metric-tile__delta--down'}`}
            >
              {delta.up ? '+' : ''}{delta.pct}%
            </span>
          )}
        </span>
        {sparkline && (
          <Sparkline values={sparkline.values} color={sparkline.color} />
        )}
      </div>
    </div>
  );
}

function CostValue({ amount, estimated }: { amount: number; estimated?: boolean }) {
  const fixed = amount.toFixed(2);
  const [whole, frac] = fixed.split('.');
  return (
    <>
      ${whole}
      <span style={{ fontSize: '0.55em', color: 'var(--grey1)', fontWeight: 400 }}>.{frac}</span>
      {estimated ? '*' : ''}
    </>
  );
}

const DELIVERY_UNIT_TOOLTIP = 'Counts delivery units (standalone tasks + parents). Subtasks are excluded.';

const COST_TOOLTIP =
  "Sum of estimated cost on cards updated in the last 30 days. Each card's full cost is attributed to its last-update day, so long-running parent cards may show as a spike on their most recent touch day - token counts as reported by agents. Values marked * include rate-table estimates.";

const CHAT_COST_TOOLTIP =
  "Server-wide chat session cost over the last 30 UTC days, bucketed by session last-active day. Cached server-side for 30 seconds.";

export function KpiRow({
  costLast30dUsd,
  costPrior30dUsd,
  costSeries30d,
  costHasEstimates,
  stateCountsParents,
  doneTodayParents,
  chatCostLast30dUsd,
  chatCostPrior30dUsd,
  chatCostSeries30d,
}: KpiRowProps) {
  const openParents = stateCountsParents['todo'] ?? 0;
  const inProgressParents = (stateCountsParents['in_progress'] ?? 0) + (stateCountsParents['review'] ?? 0);

  const hasDelta =
    Number.isFinite(costLast30dUsd) &&
    Number.isFinite(costPrior30dUsd) &&
    costPrior30dUsd > 0;
  const deltaPct = hasDelta
    ? Math.round(((costLast30dUsd - costPrior30dUsd) / costPrior30dUsd) * 100)
    : 0;
  // The rounded 0% case is treated as up to avoid red-styling tiny decreases
  // like $9.99 -> $10 (rounds to 0% but is technically negative).
  const deltaUp = hasDelta && (costLast30dUsd >= costPrior30dUsd || deltaPct === 0);

  const hasChatDelta =
    Number.isFinite(chatCostLast30dUsd) &&
    Number.isFinite(chatCostPrior30dUsd) &&
    chatCostPrior30dUsd > 0;
  const chatDeltaPct = hasChatDelta
    ? Math.round(((chatCostLast30dUsd - chatCostPrior30dUsd) / chatCostPrior30dUsd) * 100)
    : 0;
  const chatDeltaUp = hasChatDelta && (chatCostLast30dUsd >= chatCostPrior30dUsd || chatDeltaPct === 0);

  return (
    <div className="apd-kpi-row">
      <KpiTile
        label="Open tasks"
        badge="ALL"
        value={openParents}
        accent="blue"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="In progress"
        badge="ACTIVE"
        value={inProgressParents}
        accent="yellow"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="Done today"
        badge="UTC"
        value={doneTodayParents}
        accent="green"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="Cost · 30d"
        badge="USD"
        value={<CostValue amount={costLast30dUsd} estimated={costHasEstimates} />}
        accent="green"
        neutralValue
        tooltip={COST_TOOLTIP}
        delta={hasDelta ? { pct: deltaPct, up: deltaUp } : undefined}
        sparkline={costSeries30d !== undefined ? { values: costSeries30d, color: 'var(--green)' } : undefined}
      />
      <KpiTile
        label="Chat cost · 30d"
        badge="USD"
        value={<CostValue amount={chatCostLast30dUsd} />}
        accent="aqua"
        neutralValue
        tooltip={CHAT_COST_TOOLTIP}
        delta={hasChatDelta ? { pct: chatDeltaPct, up: chatDeltaUp } : undefined}
        sparkline={chatCostSeries30d !== undefined ? { values: chatCostSeries30d, color: 'var(--aqua)' } : undefined}
      />
    </div>
  );
}
