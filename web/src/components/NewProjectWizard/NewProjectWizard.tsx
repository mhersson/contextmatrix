import { useState, useEffect, useCallback } from 'react';
import { api, isAPIError } from '../../api/client';
import type { ProjectConfig, CreateProjectInput } from '../../types';

const DEFAULT_STATES = ['todo', 'in_progress', 'blocked', 'review', 'done', 'stalled', 'not_planned'];
const DEFAULT_TYPES = ['task', 'bug', 'feature'];
const DEFAULT_PRIORITIES = ['low', 'medium', 'high', 'critical'];
const DEFAULT_TRANSITIONS: Record<string, string[]> = {
  todo: ['in_progress'],
  in_progress: ['blocked', 'review', 'todo'],
  blocked: ['in_progress', 'todo'],
  review: ['done', 'in_progress'],
  done: ['todo'],
  stalled: ['todo', 'in_progress'],
  not_planned: ['todo'],
};

interface NewProjectWizardProps {
  onClose: () => void;
  onCreated: (config: ProjectConfig) => void;
}

export function NewProjectWizard({ onClose, onCreated }: NewProjectWizardProps) {
  const [name, setName] = useState('');
  const [prefix, setPrefix] = useState('');
  const [repo, setRepo] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  // Auto-derive prefix from name
  const handleNameChange = useCallback((value: string) => {
    setName(value);
    setError(null);
    // Derive prefix: uppercase, strip non-alphanumeric
    const derived = value.replace(/[^a-zA-Z0-9]/g, '').toUpperCase();
    setPrefix(derived.slice(0, 8));
  }, []);

  const handleCreate = useCallback(async () => {
    if (!name.trim() || !prefix.trim() || isSubmitting) return;
    setIsSubmitting(true);
    setError(null);
    try {
      const input: CreateProjectInput = {
        name: name.trim(),
        prefix: prefix.trim().toUpperCase(),
        states: DEFAULT_STATES,
        types: DEFAULT_TYPES,
        priorities: DEFAULT_PRIORITIES,
        transitions: DEFAULT_TRANSITIONS,
      };
      if (repo.trim()) input.repo = repo.trim();
      const config = await api.createProject(input);
      onCreated(config);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to create project');
    } finally {
      setIsSubmitting(false);
    }
  }, [name, prefix, repo, isSubmitting, onCreated]);

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
            <span className="text-sm font-medium text-[var(--fg)]">New Project</span>
          </div>
          <button
            onClick={handleCreate}
            disabled={!name.trim() || !prefix.trim() || isSubmitting}
            className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
              name.trim() && prefix.trim()
                ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
                : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
            }`}
          >
            {isSubmitting ? 'Creating...' : 'Create'}
          </button>
        </div>

        <div className="p-4 overflow-y-auto space-y-4" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          {error && (
            <div className="p-3 rounded text-sm" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
              {error}
            </div>
          )}

          <div>
            <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => handleNameChange(e.target.value)}
              placeholder="my-project"
              className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
              style={{
                backgroundColor: 'var(--bg2)',
                borderColor: 'var(--bg3)',
                color: 'var(--fg)',
              }}
              autoFocus
            />
            <p className="text-xs mt-1" style={{ color: 'var(--grey0)' }}>
              Alphanumeric with hyphens and underscores
            </p>
          </div>

          <div>
            <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Prefix</label>
            <input
              type="text"
              value={prefix}
              onChange={(e) => { setPrefix(e.target.value.toUpperCase()); setError(null); }}
              placeholder="MYPRJ"
              className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
              style={{
                backgroundColor: 'var(--bg2)',
                borderColor: 'var(--bg3)',
                color: 'var(--fg)',
              }}
            />
            <p className="text-xs mt-1" style={{ color: 'var(--grey0)' }}>
              Card ID prefix (e.g. MYPRJ-001)
            </p>
          </div>

          <div>
            <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Repository URL (optional)</label>
            <input
              type="text"
              value={repo}
              onChange={(e) => setRepo(e.target.value)}
              placeholder="git@github.com:org/repo.git"
              className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
              style={{
                backgroundColor: 'var(--bg2)',
                borderColor: 'var(--bg3)',
                color: 'var(--fg)',
              }}
            />
          </div>

          <div className="pt-2 border-t" style={{ borderColor: 'var(--bg3)' }}>
            <p className="text-xs mb-2" style={{ color: 'var(--grey1)' }}>Default configuration</p>
            <div className="space-y-2 text-xs" style={{ color: 'var(--grey2)' }}>
              <div><span style={{ color: 'var(--grey1)' }}>States:</span> {DEFAULT_STATES.join(', ')}</div>
              <div><span style={{ color: 'var(--grey1)' }}>Types:</span> {DEFAULT_TYPES.join(', ')}</div>
              <div><span style={{ color: 'var(--grey1)' }}>Priorities:</span> {DEFAULT_PRIORITIES.join(', ')}</div>
            </div>
            <p className="text-xs mt-2" style={{ color: 'var(--grey0)' }}>
              Edit these in project settings after creation.
            </p>
          </div>
        </div>
      </div>
    </>
  );
}
