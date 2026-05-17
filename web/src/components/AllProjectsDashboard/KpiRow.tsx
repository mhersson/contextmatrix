import type { CSSProperties, ReactNode } from 'react';

interface KpiRowProps {
  openTasks: number;
  inProgress: number;
  doneToday: number;
  totalCostUsd: number;
}

interface KpiTileProps {
  label: string;
  badge: string;
  value: ReactNode;
  source: string;
  accent: 'blue' | 'yellow' | 'green' | 'purple';
}

const ACCENT_TO_VAR: Record<KpiTileProps['accent'], string> = {
  blue: 'var(--blue)',
  yellow: 'var(--yellow)',
  green: 'var(--green)',
  purple: 'var(--purple)',
};

function KpiTile({ label, badge, value, source, accent }: KpiTileProps) {
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
      <div style={numStyle}>{value}</div>
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

export function KpiRow({ openTasks, inProgress, doneToday, totalCostUsd }: KpiRowProps) {
  return (
    <div
      className="apd-kpi-row"
      style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 20 }}
    >
      <KpiTile
        label="Open tasks"
        badge="ALL"
        value={openTasks}
        source="state_counts (open)"
        accent="blue"
      />
      <KpiTile
        label="In progress"
        badge="ACTIVE"
        value={inProgress}
        source="state_counts.in_progress"
        accent="yellow"
      />
      <KpiTile
        label="Done today"
        badge="UTC"
        value={doneToday}
        source="cards_completed_today"
        accent="green"
      />
      <KpiTile
        label="Total cost"
        badge="USD"
        value={<CostValue amount={totalCostUsd} />}
        source="sum(agent_costs.estimated_cost_usd)"
        accent="purple"
      />
    </div>
  );
}
