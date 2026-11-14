import type { SessionsTab } from '../../lib/sessions';
import { WaitBadge } from '../WaitBadge';

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
  /** Tabs holding an item that waits on the viewer. */
  waiting: ReadonlySet<SessionsTab>;
}

export function SessionsTabs({ active, counts, onChange, waiting }: SessionsTabsProps) {
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
            {waiting.has(t.tab) && <WaitBadge />}
          </button>
        );
      })}
    </div>
  );
}
