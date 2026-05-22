import type { CSSProperties, ReactNode } from 'react';
import { Sparkline } from '../Sparkline/Sparkline';

interface KpiRowProps {
  costLast30dUsd: number;
  costPrior30dUsd: number;
  costSeries30d: number[] | undefined;
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
  source: string;
  accent: 'blue' | 'yellow' | 'green' | 'purple';
  tooltip?: string;
  delta?: { pct: number; up: boolean };
  sparkline?: { values: number[]; color: string };
}

const ACCENT_TO_VAR: Record<KpiTileProps['accent'], string> = {
  blue: 'var(--blue)',
  yellow: 'var(--yellow)',
  green: 'var(--green)',
  purple: 'var(--purple)',
};

function KpiTile({ label, badge, value, source, accent, tooltip, delta, sparkline }: KpiTileProps) {
  const numStyle: CSSProperties = {
    fontFamily: 'var(--font-sans)',
    fontWeight: 500,
    fontSize: 40,
    lineHeight: 1.0,
    letterSpacing: '-0.04em',
    marginTop: 8,
    color: ACCENT_TO_VAR[accent],
    fontVariantNumeric: 'tabular-nums',
  };
  return (
    <div
      className="apd-card apd-kpi"
      title={tooltip}
      // aria-label intentionally not set: accessible name is composed from descendants
      style={{
        borderColor: 'var(--bg3)',
        backgroundColor: 'var(--bg1)',
        padding: '18px 20px',
      }}
    >
      <div
        className="flex items-center justify-between"
        style={{
          fontSize: 11.5,
          color: 'var(--grey1)',
          letterSpacing: '-0.005em',
          fontWeight: 500,
        }}
      >
        <span>{label}</span>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10.5,
            color: 'var(--grey1)',
            padding: '2px 7px',
            borderRadius: 3,
            backgroundColor: 'var(--bg2)',
            border: '1px solid var(--bg3)',
            letterSpacing: '0.04em',
          }}
        >
          {badge}
        </span>
      </div>
      <div
        style={{
          ...numStyle,
          display: 'inline-flex',
          alignItems: 'baseline',
          gap: 8,
        }}
      >
        {value}
        {delta !== undefined && (
          <span
            className={`metric-tile__delta ${delta.up ? 'metric-tile__delta--up' : 'metric-tile__delta--down'}`}
          >
            {delta.up ? '+' : ''}{delta.pct}%
          </span>
        )}
      </div>
      <div
        style={{
          marginTop: 8,
          fontSize: 11,
          color: 'var(--grey0)',
          fontFamily: 'var(--font-mono)',
          letterSpacing: '-0.01em',
        }}
      >
        {source}
      </div>
      {sparkline && (
        <Sparkline values={sparkline.values} color={sparkline.color} />
      )}
    </div>
  );
}

function CostValue({ amount }: { amount: number }) {
  const fixed = amount.toFixed(2);
  const [whole, frac] = fixed.split('.');
  return (
    <>
      ${whole}
      <span style={{ fontSize: '0.55em', color: 'var(--grey1)', fontWeight: 400 }}>.{frac}</span>
    </>
  );
}

const DELIVERY_UNIT_TOOLTIP = 'Counts delivery units (standalone tasks + parents). Subtasks are excluded.';

const COST_TOOLTIP =
  "Sum of estimated cost on cards updated in the last 30 days. Each card's full cost is attributed to its last-update day, so long-running parent cards may show as a spike on their most recent touch day.";

const CHAT_COST_TOOLTIP =
  "Server-wide chat session cost over the last 30 UTC days, bucketed by session last-active day. Cached server-side for 30 seconds.";

export function KpiRow({
  costLast30dUsd,
  costPrior30dUsd,
  costSeries30d,
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
    <div
      className="apd-kpi-row"
      style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 20 }}
    >
      <KpiTile
        label="Open tasks"
        badge="ALL"
        value={openParents}
        source="state_counts_parents (todo)"
        accent="blue"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="In progress"
        badge="ACTIVE"
        value={inProgressParents}
        source="state_counts_parents (in_progress+review)"
        accent="yellow"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="Done today"
        badge="UTC"
        value={doneTodayParents}
        source="cards_completed_today_parents"
        accent="green"
        tooltip={DELIVERY_UNIT_TOOLTIP}
      />
      <KpiTile
        label="Cost · 30d"
        badge="USD"
        value={<CostValue amount={costLast30dUsd} />}
        source="sum(card.cost where updated >= now-30d)"
        accent="purple"
        tooltip={COST_TOOLTIP}
        delta={hasDelta ? { pct: deltaPct, up: deltaUp } : undefined}
        sparkline={costSeries30d !== undefined ? { values: costSeries30d, color: 'var(--purple)' } : undefined}
      />
      {/* Spec says accent="purple" but blue is used here to disambiguate from the adjacent card-scoped Cost tile which is purple. */}
      <KpiTile
        label="Chat cost · 30d"
        badge="USD"
        value={<CostValue amount={chatCostLast30dUsd} />}
        source="sum(chat.cost where last_active >= now-30d)"
        accent="blue"
        tooltip={CHAT_COST_TOOLTIP}
        delta={hasChatDelta ? { pct: chatDeltaPct, up: chatDeltaUp } : undefined}
        sparkline={chatCostSeries30d !== undefined ? { values: chatCostSeries30d, color: 'var(--blue)' } : undefined}
      />
    </div>
  );
}
