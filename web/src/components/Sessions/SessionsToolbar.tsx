import type { ChatSession } from '../../types';
import { sessionProjects } from '../../lib/sessions';
import { projectChoices, type SessionFilter } from './sessionFilter';

interface SessionsToolbarProps {
  /** The current tab's sessions; the project choices come from them. */
  sessions: ChatSession[];
  filter: SessionFilter;
  onChange: (next: SessionFilter) => void;
  /** Offers Status waiting; a chat never waits. */
  offerWaiting?: boolean;
}

export function SessionsToolbar({ sessions, filter, onChange, offerWaiting = false }: SessionsToolbarProps) {
  const set = (patch: Partial<SessionFilter>) => onChange({ ...filter, ...patch });

  return (
    <div className="ssn-bar" role="search">
      <input
        type="search"
        className="ssn-search"
        placeholder="Search titles…"
        aria-label="Search titles"
        value={filter.query}
        onChange={(e) => set({ query: e.target.value })}
      />
      <label className="ssn-chip">
        Project:
        <select value={filter.project} onChange={(e) => set({ project: e.target.value })}>
          <option value="">any</option>
          {projectChoices(sessions.flatMap(sessionProjects), filter.project).map((p) => (
            <option key={p} value={p}>{p}</option>
          ))}
        </select>
      </label>
      <label className="ssn-chip">
        Status:
        <select
          value={filter.status}
          onChange={(e) => set({ status: e.target.value as SessionFilter['status'] })}
        >
          <option value="any">any</option>
          <option value="live">live</option>
          <option value="idle">not live</option>
          {offerWaiting && <option value="waiting">waiting</option>}
        </select>
      </label>
      <button
        type="button"
        className={`ssn-chip${filter.pinnedOnly ? ' ssn-chip--on' : ''}`}
        aria-pressed={filter.pinnedOnly}
        onClick={() => set({ pinnedOnly: !filter.pinnedOnly })}
      >
        Pinned only
      </button>
    </div>
  );
}
