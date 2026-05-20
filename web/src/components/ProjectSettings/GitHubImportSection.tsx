import { useId } from 'react';
import type { GitHubImportConfig } from '../../types';
import type { CSSProperties } from 'react';

export interface GitHubImportSectionProps {
  github: GitHubImportConfig;
  onChange: (gh: GitHubImportConfig) => void;
  types: string[];
  priorities: string[];
  inputStyle: CSSProperties;
}

export function GitHubImportSection({
  github,
  onChange,
  types,
  priorities,
  inputStyle,
}: GitHubImportSectionProps) {
  const update = (patch: Partial<GitHubImportConfig>) => onChange({ ...github, ...patch });
  const labelsStr = github.labels?.join(', ') ?? '';
  const ownerId = useId();
  const repoId = useId();
  const cardTypeId = useId();
  const defaultPriorityId = useId();
  const ghLabelsId = useId();
  const headingId = useId();

  return (
    <div>
      <div id={headingId} className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
        GitHub Issue Import
      </div>
      <div
        className="p-3 rounded space-y-3"
        style={{ backgroundColor: 'var(--bg1)' }}
        aria-labelledby={headingId}
      >
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={github.import_issues}
            onChange={(e) => update({ import_issues: e.target.checked })}
            className="accent-[var(--green)]"
          />
          <span className="text-sm" style={{ color: 'var(--fg)' }}>
            Import open issues from GitHub
          </span>
        </label>
        {github.import_issues && (
          <div className="space-y-3 pt-1">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label htmlFor={ownerId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
                  Owner
                </label>
                <input
                  id={ownerId}
                  type="text"
                  value={github.owner ?? ''}
                  onChange={(e) => update({ owner: e.target.value || undefined })}
                  placeholder="auto-detected from repo URL"
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={inputStyle}
                />
              </div>
              <div>
                <label htmlFor={repoId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
                  Repo
                </label>
                <input
                  id={repoId}
                  type="text"
                  value={github.repo ?? ''}
                  onChange={(e) => update({ repo: e.target.value || undefined })}
                  placeholder="auto-detected from repo URL"
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={inputStyle}
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label htmlFor={cardTypeId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
                  Card type
                </label>
                <select
                  id={cardTypeId}
                  value={github.card_type ?? ''}
                  onChange={(e) => update({ card_type: e.target.value || undefined })}
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={inputStyle}
                >
                  <option value="">task (default)</option>
                  {types.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={defaultPriorityId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
                  Default priority
                </label>
                <select
                  id={defaultPriorityId}
                  value={github.default_priority ?? ''}
                  onChange={(e) => update({ default_priority: e.target.value || undefined })}
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={inputStyle}
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
            <div>
              <label htmlFor={ghLabelsId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
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
                className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                style={inputStyle}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
