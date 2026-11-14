import type { PlaybookSummary } from '../../types';
import { projectChoices } from '../Sessions/sessionFilter';
import { PLAYBOOK_STATUSES, type PlaybookFilter } from './playbookFilter';

interface PlaybooksTabToolbarProps {
  /** Every playbook; the project choices come from them. */
  playbooks: PlaybookSummary[];
  filter: PlaybookFilter;
  onChange: (next: PlaybookFilter) => void;
}

export function PlaybooksTabToolbar({ playbooks, filter, onChange }: PlaybooksTabToolbarProps) {
  const set = (patch: Partial<PlaybookFilter>) => onChange({ ...filter, ...patch });

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
          {projectChoices(playbooks.flatMap((p) => p.project_names), filter.project).map((p) => (
            <option key={p} value={p}>{p}</option>
          ))}
        </select>
      </label>
      <label className="ssn-chip">
        Status:
        <select
          value={filter.status}
          onChange={(e) => set({ status: e.target.value as PlaybookFilter['status'] })}
        >
          {PLAYBOOK_STATUSES.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
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
