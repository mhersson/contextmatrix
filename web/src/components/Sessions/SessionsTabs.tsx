import type { SessionsTab } from '../../lib/sessions';

export const SESSIONS_PANEL_ID = 'sessions-panel';

const TABS: { tab: SessionsTab; label: string }[] = [
  { tab: 'chat', label: 'Chats' },
  { tab: 'architect', label: 'Blueprints' },
  { tab: 'playbooks', label: 'Playbooks' },
];

interface SessionsTabsProps {
  active: SessionsTab;
  counts: Record<SessionsTab, number>;
  onChange: (tab: SessionsTab) => void;
}

export function SessionsTabs({ active, counts, onChange }: SessionsTabsProps) {
  return (
    <div className="ssn-tabs" role="tablist" aria-label="Sessions">
      {TABS.map((t) => {
        const on = t.tab === active;
        return (
          <button
            key={t.tab}
            id={`sessions-tab-${t.tab}`}
            type="button"
            role="tab"
            aria-selected={on}
            aria-controls={SESSIONS_PANEL_ID}
            onClick={() => onChange(t.tab)}
            className={`ssn-tab${on ? ' ssn-tab--on' : ''}`}
          >
            {t.label}
            <span className="ssn-tab-count">{counts[t.tab]}</span>
          </button>
        );
      })}
    </div>
  );
}
