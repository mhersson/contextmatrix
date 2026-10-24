import type { SessionKind } from '../../types';

export const SESSIONS_PANEL_ID = 'sessions-panel';

const TABS: { kind: SessionKind; label: string }[] = [
  { kind: 'chat', label: 'Chats' },
  { kind: 'architect', label: 'Blueprints' },
];

interface SessionsTabsProps {
  active: SessionKind;
  counts: Record<SessionKind, number>;
  onChange: (kind: SessionKind) => void;
}

export function SessionsTabs({ active, counts, onChange }: SessionsTabsProps) {
  return (
    <div className="ssn-tabs" role="tablist" aria-label="Session kinds">
      {TABS.map((t) => {
        const on = t.kind === active;
        return (
          <button
            key={t.kind}
            id={`sessions-tab-${t.kind}`}
            type="button"
            role="tab"
            aria-selected={on}
            aria-controls={SESSIONS_PANEL_ID}
            onClick={() => onChange(t.kind)}
            className={`ssn-tab${on ? ' ssn-tab--on' : ''}`}
          >
            {t.label}
            <span className="ssn-tab-count">{counts[t.kind]}</span>
          </button>
        );
      })}
    </div>
  );
}
