import { useCallback, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { ActiveAgent, ModelCost } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import {
  agentInitials,
  compactSeconds,
  isHumanAgent,
  medianHeartbeatSeconds,
  oldestClaim,
  projectForCardId,
} from './utils';

interface CostAgentsPanelProps {
  modelCosts: ModelCost[];
  activeAgents: ActiveAgent[];
  stalledCount: number;
  prefixMap: Map<string, string>;
}

type Tab = 'models' | 'agents';

const TOP_MODEL_COSTS = 5;
const TAB_IDS: Record<Tab, { btn: string; panel: string }> = {
  models: { btn: 'apd-tab-models-btn', panel: 'apd-tab-models-panel' },
  agents: { btn: 'apd-tab-agents-btn', panel: 'apd-tab-agents-panel' },
};

function CostByModel({ modelCosts }: { modelCosts: ModelCost[] }) {
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

const agentRowStyle = {
  display: 'grid',
  gridTemplateColumns: 'auto 1fr auto',
  gap: 12,
  alignItems: 'center',
  padding: '11px 12px',
  borderRadius: 5,
  textAlign: 'left' as const,
  textDecoration: 'none',
};

function AgentsOnDuty({
  activeAgents,
  stalledCount,
  prefixMap,
}: {
  activeAgents: ActiveAgent[];
  stalledCount: number;
  prefixMap: Map<string, string>;
}) {
  const median = medianHeartbeatSeconds(activeAgents);
  const oldest = oldestClaim(activeAgents);

  return (
    <>
      {activeAgents.length === 0 ? (
        <div
          style={{
            padding: '32px 20px',
            textAlign: 'center',
            fontSize: 12.5,
            color: 'var(--grey0)',
            fontStyle: 'italic',
          }}
        >
          No agents currently active
        </div>
      ) : (
        <div style={{ padding: 8 }}>
          {activeAgents.map((a) => {
            const human = isHumanAgent(a.agent_id);
            const project = projectForCardId(a.card_id, prefixMap);
            const lastBeat = a.last_heartbeat
              ? formatRelativeTime(a.last_heartbeat)
              : 'no beat';
            const inner = (
              <>
                <span
                  className="apd-agent-avatar"
                  style={{
                    backgroundColor: human ? 'var(--bg-blue)' : 'var(--bg-aqua)',
                    color: human ? 'var(--blue)' : 'var(--aqua)',
                  }}
                  aria-hidden="true"
                >
                  {agentInitials(a.agent_id)}
                </span>
                <span className="min-w-0">
                  <span
                    className="flex items-center gap-1.5"
                    style={{
                      fontSize: 13,
                      color: 'var(--fg)',
                      fontWeight: 500,
                      letterSpacing: '-0.01em',
                    }}
                  >
                    <span className="truncate">{a.agent_id}</span>
                    <span
                      className="apd-role-tag"
                      style={{
                        color: 'var(--grey1)',
                        backgroundColor: 'var(--bg2)',
                        border: '1px solid var(--bg3)',
                      }}
                    >
                      {human ? 'Human' : 'AI'}
                    </span>
                  </span>
                  <span
                    className="block truncate"
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 11.5,
                      color: 'var(--grey1)',
                      letterSpacing: '-0.01em',
                      marginTop: 2,
                    }}
                  >
                    <span style={{ color: 'var(--aqua)', fontWeight: 500 }}>
                      {a.card_id}
                    </span>{' '}
                    · {a.card_title}
                  </span>
                </span>
                <span
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 11,
                    color: 'var(--grey1)',
                    textAlign: 'right',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <span style={{ display: 'block', color: 'var(--grey0)', fontSize: 9.5 }}>
                    last beat
                  </span>
                  {lastBeat}
                </span>
              </>
            );
            return project ? (
              <Link
                key={`${a.agent_id}-${a.card_id}`}
                to={`/projects/${project}/dashboard`}
                className="apd-agent-row"
                style={agentRowStyle}
              >
                {inner}
              </Link>
            ) : (
              <div
                key={`${a.agent_id}-${a.card_id}`}
                className="apd-agent-row apd-agent-row-static"
                style={agentRowStyle}
              >
                {inner}
              </div>
            );
          })}
        </div>
      )}
      <div
        className="apd-agents-footer"
        style={{ borderTop: '1px solid var(--bg2)' }}
      >
        <FooterCell
          label="Median heartbeat"
          value={median !== null ? compactSeconds(median) : '—'}
        />
        <FooterCell label="Oldest claim" value={oldest} />
        <FooterCell
          label="Stalled"
          value={String(stalledCount)}
          valueColor={stalledCount > 0 ? 'var(--red)' : 'var(--fg)'}
        />
      </div>
    </>
  );
}

function FooterCell({
  label,
  value,
  valueColor,
}: {
  label: string;
  value: string;
  valueColor?: string;
}) {
  return (
    <div style={{ backgroundColor: 'var(--bg1)', padding: '13px 16px' }}>
      <div style={{ fontSize: 10.5, color: 'var(--grey1)', fontWeight: 500 }}>{label}</div>
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontWeight: 500,
          fontSize: 20,
          color: valueColor ?? 'var(--fg)',
          marginTop: 3,
          fontVariantNumeric: 'tabular-nums',
          letterSpacing: '-0.015em',
        }}
      >
        {value}
      </div>
    </div>
  );
}

export function CostAgentsPanel({
  modelCosts,
  activeAgents,
  stalledCount,
  prefixMap,
}: CostAgentsPanelProps) {
  const [tab, setTab] = useState<Tab>('models');
  const tabListRef = useRef<HTMLDivElement>(null);

  const tabs = useMemo<{ id: Tab; label: string; count: number }[]>(
    () => [
      {
        id: 'models',
        label: 'Models',
        count: modelCosts.length,
      },
      { id: 'agents', label: 'Agents on duty', count: activeAgents.length },
    ],
    [modelCosts.length, activeAgents.length],
  );

  // ARIA tabs keyboard pattern: Left/Right cycle, Home/End jump to ends.
  const onTabKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLButtonElement>) => {
      const order: Tab[] = tabs.map((t) => t.id);
      const idx = order.indexOf(tab);
      let nextIdx: number;
      switch (e.key) {
        case 'ArrowRight':
          nextIdx = (idx + 1) % order.length;
          break;
        case 'ArrowLeft':
          nextIdx = (idx - 1 + order.length) % order.length;
          break;
        case 'Home':
          nextIdx = 0;
          break;
        case 'End':
          nextIdx = order.length - 1;
          break;
        default:
          return;
      }
      e.preventDefault();
      const nextTab = order[nextIdx];
      setTab(nextTab);
      // Move DOM focus to the newly selected tab so screen-reader focus follows.
      const btn = tabListRef.current?.querySelector<HTMLButtonElement>(
        `#${TAB_IDS[nextTab].btn}`,
      );
      btn?.focus();
    },
    [tab, tabs],
  );

  return (
    <section
      className="apd-card"
      style={{
        borderColor: 'var(--bg3)',
        backgroundColor: 'var(--bg1)',
        overflow: 'hidden',
      }}
    >
      <div
        ref={tabListRef}
        className="apd-tab-strip"
        role="tablist"
        aria-label="Cost and agents views"
        aria-orientation="horizontal"
        style={{
          borderBottom: '1px solid var(--bg2)',
          backgroundColor: 'var(--bg1)',
        }}
      >
        {tabs.map((t) => {
          const on = tab === t.id;
          const isModels = t.id === 'models';
          return (
            <div
              key={t.id}
              style={{ display: 'inline-flex', alignItems: 'center' }}
            >
              <button
                id={TAB_IDS[t.id].btn}
                type="button"
                role="tab"
                aria-selected={on}
                aria-controls={TAB_IDS[t.id].panel}
                tabIndex={on ? 0 : -1}
                onClick={() => setTab(t.id)}
                onKeyDown={onTabKeyDown}
                className="apd-tab-btn"
                style={{
                  color: on ? 'var(--fg)' : 'var(--grey1)',
                  borderBottomColor: on ? 'var(--aqua)' : 'transparent',
                }}
              >
                <span>{t.label}</span>
                <span
                  className="apd-tab-count"
                  style={{
                    color: on ? 'var(--aqua)' : 'var(--grey1)',
                    backgroundColor: on ? 'var(--bg-aqua)' : 'var(--bg2)',
                    border: on ? '1px solid transparent' : '1px solid var(--bg3)',
                  }}
                >
                  {t.count}
                </span>
              </button>
              {isModels && (
                <button
                  type="button"
                  aria-label="Each card is attributed to its most-recently-used model. Cards that used multiple models show under the last one."
                  title="Each card is attributed to its most-recently-used model. Cards that used multiple models show under the last one."
                  style={{
                    marginLeft: 6,
                    marginRight: 4,
                    color: 'var(--grey1)',
                    cursor: 'help',
                    background: 'none',
                    border: 'none',
                    padding: 0,
                    font: 'inherit',
                    lineHeight: 1,
                  }}
                >
                  <span aria-hidden="true">&#9432;</span>
                </button>
              )}
            </div>
          );
        })}
      </div>
      <div
        role="tabpanel"
        id={TAB_IDS.models.panel}
        aria-labelledby={TAB_IDS.models.btn}
        hidden={tab !== 'models'}
        tabIndex={0}
      >
        {tab === 'models' && <CostByModel modelCosts={modelCosts} />}
      </div>
      <div
        role="tabpanel"
        id={TAB_IDS.agents.panel}
        aria-labelledby={TAB_IDS.agents.btn}
        hidden={tab !== 'agents'}
        tabIndex={0}
      >
        {tab === 'agents' && (
          <AgentsOnDuty
            activeAgents={activeAgents}
            stalledCount={stalledCount}
            prefixMap={prefixMap}
          />
        )}
      </div>
    </section>
  );
}
