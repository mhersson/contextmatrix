import { useState, useEffect, useCallback } from 'react';
import { api, isAPIError } from '../../api/client';
import type { JiraEpicPreview, JiraImportResult } from '../../types';
import { jiraIcon } from '../icons';

interface JiraImportWizardProps {
  onClose: () => void;
  onImported: (result: JiraImportResult) => void;
}

export function JiraImportWizard({ onClose, onImported }: JiraImportWizardProps) {
  const [epicKey, setEpicKey] = useState('');
  const [preview, setPreview] = useState<JiraEpicPreview | null>(null);
  const [name, setName] = useState('');
  const [prefix, setPrefix] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handlePreview = useCallback(async () => {
    if (!epicKey.trim() || isLoading) return;
    setIsLoading(true);
    setError(null);
    setPreview(null);
    try {
      const result = await api.previewJiraEpic(epicKey.trim().toUpperCase());
      setPreview(result);
      // Auto-derive project name and prefix from epic.
      const slug = result.epic.summary
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-|-$/g, '')
        .slice(0, 50);
      setName(slug || 'jira-import');
      // Include epic number in prefix to avoid collisions across epics
      // from the same Jira project (e.g., PROJ-42 → PROJ42).
      const key = epicKey.trim().toUpperCase().replaceAll('-', '');
      setPrefix(key);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to fetch epic from Jira');
    } finally {
      setIsLoading(false);
    }
  }, [epicKey, isLoading]);

  const handleImport = useCallback(async () => {
    if (!epicKey.trim() || !name.trim() || !prefix.trim() || isImporting) return;
    setIsImporting(true);
    setError(null);
    try {
      const result = await api.importJiraEpic({
        epic_key: epicKey.trim().toUpperCase(),
        name: name.trim(),
        prefix: prefix.trim().toUpperCase(),
      });
      onImported(result);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to import epic');
    } finally {
      setIsImporting(false);
    }
  }, [epicKey, name, prefix, isImporting, onImported]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && !preview) handlePreview();
    },
    [preview, handlePreview]
  );

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />
      <div className="card-panel animate-panel-slide-in">
        <div className="flex items-center justify-between p-4 border-b border-[var(--bg3)]">
          <div className="flex items-center gap-3">
            <button
              onClick={onClose}
              className="text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
              title="Close (Esc)"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
            <span className="flex items-center gap-2 text-sm font-medium text-[var(--fg)]">
              {jiraIcon} Import from Jira
            </span>
          </div>
          {preview && (
            <button
              onClick={handleImport}
              disabled={!name.trim() || !prefix.trim() || isImporting}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                name.trim() && prefix.trim()
                  ? 'bg-[var(--blue)] text-[var(--bg-dim)] hover:opacity-90'
                  : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
              }`}
            >
              {isImporting ? 'Importing...' : `Import ${preview.children.filter(c => !c.done).length} issue${preview.children.filter(c => !c.done).length === 1 ? '' : 's'}`}
            </button>
          )}
        </div>

        <div className="p-4 overflow-y-auto space-y-4" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          {error && (
            <div className="p-3 rounded text-sm" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
              {error}
            </div>
          )}

          {/* Step 1: Epic key input */}
          {!preview && (
            <div>
              <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Jira Epic Key</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={epicKey}
                  onChange={(e) => { setEpicKey(e.target.value); setError(null); }}
                  onKeyDown={handleKeyDown}
                  placeholder="PROJ-42"
                  className="flex-1 px-3 py-2 rounded text-sm border focus:outline-none"
                  style={{
                    backgroundColor: 'var(--bg2)',
                    borderColor: 'var(--bg3)',
                    color: 'var(--fg)',
                  }}
                  autoFocus
                />
                <button
                  onClick={handlePreview}
                  disabled={!epicKey.trim() || isLoading}
                  className={`px-4 py-2 rounded text-sm font-medium transition-colors ${
                    epicKey.trim()
                      ? 'bg-[var(--blue)] text-[var(--bg-dim)] hover:opacity-90'
                      : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
                  }`}
                >
                  {isLoading ? 'Loading...' : 'Preview'}
                </button>
              </div>
              <p className="text-xs mt-1" style={{ color: 'var(--grey0)' }}>
                Enter a Jira epic key to preview its child issues
              </p>
            </div>
          )}

          {/* Step 2: Preview + configure */}
          {preview && (
            <>
              <div className="p-3 rounded" style={{ backgroundColor: 'var(--bg1)' }}>
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-xs px-1.5 py-0.5 rounded" style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--blue)' }}>
                    {preview.epic.issue_type}
                  </span>
                  <span className="text-sm font-medium" style={{ color: 'var(--fg)' }}>
                    {preview.epic.key}: {preview.epic.summary}
                  </span>
                </div>
                <span className="text-xs" style={{ color: 'var(--grey1)' }}>
                  {preview.children.filter(c => !c.done).length} to import{preview.children.some(c => c.done) ? `, ${preview.children.filter(c => c.done).length} already done` : ''} | Status: {preview.epic.status}
                </span>
              </div>

              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Project Name</label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => { setName(e.target.value); setError(null); }}
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={{
                    backgroundColor: 'var(--bg2)',
                    borderColor: 'var(--bg3)',
                    color: 'var(--fg)',
                  }}
                />
              </div>

              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Prefix</label>
                <input
                  type="text"
                  value={prefix}
                  onChange={(e) => { setPrefix(e.target.value.toUpperCase()); setError(null); }}
                  className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
                  style={{
                    backgroundColor: 'var(--bg2)',
                    borderColor: 'var(--bg3)',
                    color: 'var(--fg)',
                  }}
                />
                <p className="text-xs mt-1" style={{ color: 'var(--grey0)' }}>
                  Card ID prefix (e.g. {prefix || 'PROJ'}-001)
                </p>
              </div>

              {preview.children.length > 0 && (
                <div>
                  <label className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
                    Issues to import
                  </label>
                  <div
                    className="rounded border overflow-y-auto"
                    style={{
                      borderColor: 'var(--bg3)',
                      maxHeight: 'calc(100vh - 420px)',
                    }}
                  >
                    {preview.children.map((child) => (
                      <div
                        key={child.key}
                        className={`flex items-center gap-2 px-3 py-2 border-b last:border-b-0 text-sm${child.done ? ' opacity-40' : ''}`}
                        style={{ borderColor: 'var(--bg3)', backgroundColor: 'var(--bg1)' }}
                      >
                        <span className="font-mono text-xs flex-shrink-0" style={{ color: 'var(--grey1)' }}>
                          {child.key}
                        </span>
                        <span className="text-xs px-1 py-0.5 rounded flex-shrink-0" style={{
                          backgroundColor: child.issue_type === 'Bug' ? 'var(--bg-red)' : 'var(--bg-blue)',
                          color: child.issue_type === 'Bug' ? 'var(--red)' : 'var(--blue)',
                        }}>
                          {child.issue_type}
                        </span>
                        <span className="truncate" style={{ color: 'var(--fg)' }}>
                          {child.summary}
                        </span>
                        <span className="ml-auto text-xs flex-shrink-0" style={{ color: 'var(--grey0)' }}>
                          {child.done ? 'skipped' : child.status}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {preview.children.length === 0 && (
                <div className="text-sm text-center py-4" style={{ color: 'var(--grey0)' }}>
                  This epic has no child issues. An empty project will be created.
                </div>
              )}

              <button
                onClick={() => { setPreview(null); setError(null); }}
                className="text-xs hover:underline"
                style={{ color: 'var(--grey1)' }}
              >
                Change epic key
              </button>
            </>
          )}
        </div>
      </div>
    </>
  );
}
