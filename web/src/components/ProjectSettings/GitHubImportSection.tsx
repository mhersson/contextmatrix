import { useId } from 'react';
import type { GitHubImportConfig } from '../../types';

export interface GitHubImportSectionProps {
  github: GitHubImportConfig;
  onChange: (gh: GitHubImportConfig) => void;
  types: string[];
  priorities: string[];
}

export function GitHubImportSection({ github, onChange, types, priorities }: GitHubImportSectionProps) {
  const update = (patch: Partial<GitHubImportConfig>) => onChange({ ...github, ...patch });
  const labelsStr = github.labels?.join(', ') ?? '';
  const ownerId = useId();
  const repoId = useId();
  const cardTypeId = useId();
  const defaultPriorityId = useId();
  const ghLabelsId = useId();

  return (
    <>
      <div className="bf-auto-stack">
        <div className="bf-spread">
          <label className="bf-switch">
            <input
              type="checkbox"
              checked={github.import_issues}
              onChange={(e) => update({ import_issues: e.target.checked })}
            />
            <span>Import open issues from GitHub</span>
          </label>
        </div>
      </div>
      {github.import_issues && (
        <div className="space-y-3 pt-2">
          <div className="ps-two">
            <div className="ps-field">
              <label htmlFor={ownerId} className="ps-label">
                Owner
              </label>
              <input
                id={ownerId}
                type="text"
                value={github.owner ?? ''}
                onChange={(e) => update({ owner: e.target.value || undefined })}
                placeholder="auto-detected from repo URL"
                className="ps-input"
              />
            </div>
            <div className="ps-field">
              <label htmlFor={repoId} className="ps-label">
                Repo
              </label>
              <input
                id={repoId}
                type="text"
                value={github.repo ?? ''}
                onChange={(e) => update({ repo: e.target.value || undefined })}
                placeholder="auto-detected from repo URL"
                className="ps-input"
              />
            </div>
          </div>
          <div className="ps-two">
            <div className="ps-field">
              <label htmlFor={cardTypeId} className="ps-label">
                Card type
              </label>
              <select
                id={cardTypeId}
                value={github.card_type ?? ''}
                onChange={(e) => update({ card_type: e.target.value || undefined })}
                className="ps-select"
              >
                <option value="">task (default)</option>
                {types.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </div>
            <div className="ps-field">
              <label htmlFor={defaultPriorityId} className="ps-label">
                Default priority
              </label>
              <select
                id={defaultPriorityId}
                value={github.default_priority ?? ''}
                onChange={(e) => update({ default_priority: e.target.value || undefined })}
                className="ps-select"
              >
                <option value="">medium (default)</option>
                {priorities.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="ps-field">
            <label htmlFor={ghLabelsId} className="ps-label">
              Filter by GitHub labels
            </label>
            <input
              id={ghLabelsId}
              type="text"
              value={labelsStr}
              onChange={(e) => {
                const val = e.target.value;
                update({
                  labels: val
                    ? val
                        .split(',')
                        .map((l) => l.trim())
                        .filter(Boolean)
                    : undefined,
                });
              }}
              placeholder="comma-separated, e.g. bug, help wanted (empty = all)"
              className="ps-input"
            />
          </div>
        </div>
      )}
    </>
  );
}
