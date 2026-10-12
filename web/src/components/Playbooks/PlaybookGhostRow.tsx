import type { KeyboardEvent } from 'react';
import { parseJiraRef } from '../../lib/jiraRef';
import type { BoardsRepoInfo } from '../../types';

const plusIcon = (
  <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
    <path d="M6 1.5v9M1.5 6h9" />
  </svg>
);

interface NewPlaybookButtonProps {
  onClick: () => void;
  disabled?: boolean;
  className?: string;
}

export function NewPlaybookButton({ onClick, disabled, className }: NewPlaybookButtonProps) {
  return (
    <button type="button" onClick={onClick} disabled={disabled} className={`pbl-btn-new ${className ?? ''}`}>
      {plusIcon} New playbook
    </button>
  );
}

export function PlaceholderTrack({ nodes, gate }: { nodes: number; gate: number }) {
  return (
    <div className="pbl-track pbl-track-placeholder" aria-hidden="true">
      {Array.from({ length: nodes }, (_, i) => (
        <span key={i} className="contents">
          <span className={`pbl-node pbl-node-pending${i === gate ? ' pbl-node-gate' : ''}`} />
          {i < nodes - 1 && <span className="pbl-rail pbl-rail-dash" />}
        </span>
      ))}
    </div>
  );
}

export interface PlaybookGhostRowProps {
  title: string;
  onTitleChange: (value: string) => void;
  description: string;
  onDescriptionChange: (value: string) => void;
  onCreate: () => void;
  onCancel: () => void;
  submitting: boolean;
  repos?: BoardsRepoInfo[];
  repo?: string;
  onRepoChange?: (repo: string) => void;
  /** The Jira site when Jira is configured; shows "Import from Jira". */
  jiraSiteURL?: string;
  /** Why the configured Jira cannot be used; shown, and the import button stays disabled. */
  jiraAuthProblem?: string;
  onImportJira?: (epicKey: string) => void;
}

export function PlaybookGhostRow({
  title, onTitleChange, description, onDescriptionChange, onCreate, onCancel, submitting, repos, repo, onRepoChange,
  jiraSiteURL, jiraAuthProblem, onImportJira,
}: PlaybookGhostRowProps) {
  const epicKey = jiraSiteURL ? parseJiraRef(title, jiraSiteURL) : null;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') onCreate();
    if (e.key === 'Escape') onCancel();
  };

  return (
    <div className="pbl-ghost">
      <div className="pbl-ghost-top">
        {repos && repos.length > 1 && (
          <label className="pbl-repo-chip">
            boards
            <select
              aria-label="Boards repo"
              value={repo ?? repos[0].name}
              onChange={(e) => onRepoChange?.(e.target.value)}
            >
              {repos.map((r) => (
                <option key={r.name} value={r.name}>{r.name}{r.shared ? ' (shared)' : ''}</option>
              ))}
            </select>
          </label>
        )}
        <span className="pbl-ghost-hint">Cards and manual steps are added on the next screen</span>
      </div>

      <input
        autoFocus
        value={title}
        onChange={(e) => onTitleChange(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={jiraSiteURL ? 'Name this playbook, or paste a Jira epic key' : 'Name this playbook'}
        aria-label="Playbook title"
        className="pbl-ghost-title"
      />
      <input
        value={description}
        onChange={(e) => onDescriptionChange(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder="What this route is for, in a sentence"
        aria-label="Description"
        className="pbl-ghost-desc"
      />

      <PlaceholderTrack nodes={6} gate={3} />

      <div className="pbl-ghost-foot">
        {jiraAuthProblem && (
          <span role="status" className="text-xs" style={{ color: 'var(--yellow)' }}>{jiraAuthProblem}</span>
        )}
        <span className="pbl-ghost-keys"><kbd className="pbl-kbd">Enter</kbd> creates, <kbd className="pbl-kbd">Esc</kbd> cancels</span>
        <button type="button" onClick={onCancel} className="pbl-btn-quiet">Cancel</button>
        {jiraSiteURL && (
          <button
            type="button"
            onClick={() => epicKey && onImportJira?.(epicKey)}
            disabled={!epicKey || !!jiraAuthProblem || submitting}
            className="pbl-btn-quiet"
          >
            Import from Jira
          </button>
        )}
        <button type="button" onClick={onCreate} disabled={submitting} className="pbl-btn-new">Create playbook</button>
      </div>
    </div>
  );
}
