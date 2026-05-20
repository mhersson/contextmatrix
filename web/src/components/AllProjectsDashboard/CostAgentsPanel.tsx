import { useCallback, useMemo, useRef, useState } from 'react';
import type { ActiveAgent, ModelCost } from '../../types';
import { AgentsOnDuty } from './AgentsOnDuty';
import { CostByModel } from './CostByModel';

interface CostAgentsPanelProps {
  modelCosts: ModelCost[];
  activeAgents: ActiveAgent[];
  stalledCount: number;
  prefixMap: Map<string, string>;
}

type Tab = 'models' | 'agents';

const TAB_IDS: Record<Tab, { btn: string; panel: string }> = {
  models: { btn: 'apd-tab-models-btn', panel: 'apd-tab-models-panel' },
  agents: { btn: 'apd-tab-agents-btn', panel: 'apd-tab-agents-panel' },
};

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
