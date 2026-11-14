import type { PlaybookSummary } from '../../types';
import { projectChoices } from '../Sessions/sessionFilter';
import { PLAYBOOK_STATUSES, type PlaybookFilter } from './playbookFilter';

interface PlaybooksTabToolbarProps {
  /** Every playbook; the project choices come from them. */
  playbooks: PlaybookSummary[];
  filter: PlaybookFilter;
  onChange: (next: PlaybookFilter) => void;
  /** Opens the create row; Import from Jira opens it too, for a key or URL. */
  onNew: () => void;
  creating: boolean;
  jiraEnabled: boolean;
}

export function PlaybooksTabToolbar({ playbooks, filter, onChange, onNew, creating, jiraEnabled }: PlaybooksTabToolbarProps) {
  const set = (patch: Partial<PlaybookFilter>) => onChange({ ...filter, ...patch });

  return (
    <div className="pbt-bar">
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
      {jiraEnabled && (
        <button type="button" className="pbl-btn-quiet" onClick={onNew} disabled={creating}>
          Import from Jira
        </button>
      )}
      <button type="button" className="pbl-btn-new" onClick={onNew} disabled={creating}>
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
          <path d="M6 1.5v9M1.5 6h9" />
        </svg>
        New playbook
      </button>
    </div>
  );
}
