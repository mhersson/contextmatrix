import type { KeyboardEvent } from 'react';
import { parseJiraRef } from '../../lib/jiraRef';
import type { BoardsRepoInfo } from '../../types';

export interface PlaybookCreateRowProps {
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
  /** Why the configured Jira may not work; shown as a warning. */
  jiraAuthProblem?: string;
  /** Jira rejects the token, so the import button stays disabled. */
  jiraImportBlocked?: boolean;
  onImportJira?: (epicKey: string) => void;
}

/** The new-playbook form as the first row of the Playbooks table. */
export function PlaybookCreateRow({
  title, onTitleChange, description, onDescriptionChange, onCreate, onCancel, submitting, repos, repo, onRepoChange,
  jiraSiteURL, jiraAuthProblem, jiraImportBlocked, onImportJira,
}: PlaybookCreateRowProps) {
  const epicKey = jiraSiteURL ? parseJiraRef(title, jiraSiteURL) : null;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !e.nativeEvent.isComposing) onCreate();
    if (e.key === 'Escape') onCancel();
  };

  return (
    <tr className="pbt-create">
      <td colSpan={2} />
      <td colSpan={5}>
        <div className="pbt-create-fields">
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
          <input
            autoFocus
            value={title}
            onChange={(e) => onTitleChange(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder={jiraSiteURL ? 'Playbook title, or a Jira epic key or URL' : 'Playbook title'}
            aria-label="Playbook title"
            className="pbt-create-input pbt-create-title"
          />
          <input
            value={description}
            onChange={(e) => onDescriptionChange(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder="What this route is for, in a sentence"
            aria-label="Description"
            className="pbt-create-input"
          />
        </div>
        <div className="pbt-create-foot">
          {jiraAuthProblem && <span role="status" className="pbt-create-warn">{jiraAuthProblem}</span>}
          <span className="pbt-create-keys">
            <kbd className="pbl-kbd">Enter</kbd> creates, <kbd className="pbl-kbd">Esc</kbd> cancels
          </span>
          <button type="button" onClick={onCancel} className="pbl-btn-quiet">Cancel</button>
          {jiraSiteURL && (
            <button
              type="button"
              onClick={() => epicKey && onImportJira?.(epicKey)}
              disabled={!epicKey || !!jiraImportBlocked || submitting}
              className="pbl-btn-quiet"
            >
              Import from Jira
            </button>
          )}
          <button type="button" onClick={onCreate} disabled={submitting} className="pbl-btn-new">Create playbook</button>
        </div>
      </td>
    </tr>
  );
}
