import { useState, useCallback, useEffect } from 'react';
import { api, isAPIError } from '../../api/client';
import type { ProjectConfig, UpdateProjectInput } from '../../types';

interface ProjectSettingsProps {
  project: string;
  onUpdated: (config: ProjectConfig) => void;
  onDeleted: () => void;
  showToast: (message: string, type: 'success' | 'error' | 'info') => void;
}

export function ProjectSettings({ project, onUpdated, onDeleted, showToast }: ProjectSettingsProps) {
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [repo, setRepo] = useState('');
  const [states, setStates] = useState<string[]>([]);
  const [types, setTypes] = useState<string[]>([]);
  const [priorities, setPriorities] = useState<string[]>([]);
  const [transitions, setTransitions] = useState<Record<string, string[]>>({});
  const [newState, setNewState] = useState('');
  const [newType, setNewType] = useState('');
  const [newPriority, setNewPriority] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [cardCount, setCardCount] = useState(0);

  useEffect(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      api.getProject(project),
      api.getCards(project).then(cards => cards.length),
    ])
      .then(([cfg, count]) => {
        setConfig(cfg);
        setRepo(cfg.repo || '');
        setStates(cfg.states);
        setTypes(cfg.types);
        setPriorities(cfg.priorities);
        setTransitions(cfg.transitions);
        setCardCount(count);
      })
      .catch(err => setError(isAPIError(err) ? err.error : 'Failed to load project'))
      .finally(() => setLoading(false));
  }, [project]);

  const isDirty = useCallback(() => {
    if (!config) return false;
    return (
      repo !== (config.repo || '') ||
      JSON.stringify(states) !== JSON.stringify(config.states) ||
      JSON.stringify(types) !== JSON.stringify(config.types) ||
      JSON.stringify(priorities) !== JSON.stringify(config.priorities) ||
      JSON.stringify(transitions) !== JSON.stringify(config.transitions)
    );
  }, [config, repo, states, types, priorities, transitions]);

  const handleSave = useCallback(async () => {
    if (!isDirty() || isSaving) return;
    setIsSaving(true);
    try {
      const input: UpdateProjectInput = {
        repo: repo || undefined,
        states,
        types,
        priorities,
        transitions,
      };
      const updated = await api.updateProject(project, input);
      setConfig(updated);
      onUpdated(updated);
      showToast('Project settings saved', 'success');
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to save', 'error');
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, repo, states, types, priorities, transitions, project, onUpdated, showToast]);

  const handleDelete = useCallback(async () => {
    if (isDeleting) return;
    setIsDeleting(true);
    try {
      await api.deleteProject(project);
      showToast(`Project "${project}" deleted`, 'success');
      onDeleted();
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to delete', 'error');
    } finally {
      setIsDeleting(false);
      setConfirmDelete(false);
    }
  }, [isDeleting, project, onDeleted, showToast]);

  const addItem = (list: string[], setter: (v: string[]) => void, value: string, clear: (v: string) => void) => {
    const trimmed = value.trim();
    if (trimmed && !list.includes(trimmed)) {
      setter([...list, trimmed]);
      clear('');
    }
  };

  const removeItem = (list: string[], setter: (v: string[]) => void, value: string) => {
    setter(list.filter(v => v !== value));
    // Also clean transitions when removing a state
    if (list === states) {
      const newTransitions = { ...transitions };
      delete newTransitions[value];
      for (const key of Object.keys(newTransitions)) {
        newTransitions[key] = newTransitions[key].filter(s => s !== value);
      }
      setTransitions(newTransitions);
    }
  };

  const toggleTransition = (from: string, to: string) => {
    const current = transitions[from] || [];
    const newTargets = current.includes(to)
      ? current.filter(s => s !== to)
      : [...current, to];
    setTransitions({ ...transitions, [from]: newTargets });
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full" style={{ color: 'var(--grey1)' }}>
        Loading project settings...
      </div>
    );
  }

  if (error || !config) {
    return (
      <div className="p-4 rounded m-4" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
        {error || 'Project not found'}
      </div>
    );
  }

  const inputStyle = {
    backgroundColor: 'var(--bg2)',
    borderColor: 'var(--bg3)',
    color: 'var(--fg)',
  };

  return (
    <div className="p-6 overflow-y-auto h-full max-w-3xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          Project Settings
        </h2>
        <button
          onClick={handleSave}
          disabled={!isDirty() || isSaving}
          className={`px-4 py-1.5 rounded text-sm font-medium transition-colors ${
            isDirty()
              ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
              : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
          }`}
        >
          {isSaving ? 'Saving...' : 'Save'}
        </button>
      </div>

      {/* Read-only fields */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Name</label>
          <div className="px-3 py-2 rounded text-sm" style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}>
            {config.name}
          </div>
        </div>
        <div>
          <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Prefix</label>
          <div className="px-3 py-2 rounded text-sm" style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}>
            {config.prefix}
          </div>
        </div>
      </div>

      {/* Repo */}
      <div>
        <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Repository URL</label>
        <input
          type="text"
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          placeholder="git@github.com:org/repo.git"
          className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
          style={inputStyle}
        />
      </div>

      {/* States */}
      <ListEditor
        label="States"
        items={states}
        newValue={newState}
        setNewValue={setNewState}
        onAdd={() => addItem(states, setStates, newState, setNewState)}
        onRemove={(v) => removeItem(states, setStates, v)}
        protectedItems={['stalled']}
        inputStyle={inputStyle}
      />

      {/* Types */}
      <ListEditor
        label="Types"
        items={types}
        newValue={newType}
        setNewValue={setNewType}
        onAdd={() => addItem(types, setTypes, newType, setNewType)}
        onRemove={(v) => removeItem(types, setTypes, v)}
        inputStyle={inputStyle}
      />

      {/* Priorities */}
      <ListEditor
        label="Priorities"
        items={priorities}
        newValue={newPriority}
        setNewValue={setNewPriority}
        onAdd={() => addItem(priorities, setPriorities, newPriority, setNewPriority)}
        onRemove={(v) => removeItem(priorities, setPriorities, v)}
        inputStyle={inputStyle}
      />

      {/* Transitions */}
      <div>
        <label className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>Transitions</label>
        <div className="space-y-2">
          {states.map((from) => (
            <div key={from} className="p-3 rounded" style={{ backgroundColor: 'var(--bg1)' }}>
              <div className="text-xs font-medium mb-1.5" style={{ color: 'var(--fg)' }}>{from}</div>
              <div className="flex flex-wrap gap-1.5">
                {states.filter(s => s !== from).map((to) => (
                  <button
                    key={to}
                    onClick={() => toggleTransition(from, to)}
                    className="px-2 py-0.5 rounded text-xs transition-colors"
                    style={{
                      backgroundColor: (transitions[from] || []).includes(to) ? 'var(--bg-green)' : 'var(--bg2)',
                      color: (transitions[from] || []).includes(to) ? 'var(--green)' : 'var(--grey1)',
                    }}
                  >
                    {to}
                  </button>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Danger zone */}
      <div className="pt-4 border-t" style={{ borderColor: 'var(--bg3)' }}>
        <h3 className="text-sm font-medium mb-2" style={{ color: 'var(--red)' }}>Danger Zone</h3>
        {cardCount > 0 ? (
          <p className="text-xs mb-2" style={{ color: 'var(--grey1)' }}>
            Cannot delete this project — it has {cardCount} card{cardCount !== 1 ? 's' : ''}. Delete all cards first.
          </p>
        ) : null}
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <button
              onClick={handleDelete}
              disabled={isDeleting || cardCount > 0}
              className="px-3 py-1.5 rounded text-sm font-medium transition-colors"
              style={{ backgroundColor: 'var(--red)', color: 'var(--bg-dim)' }}
            >
              {isDeleting ? 'Deleting...' : 'Confirm Delete'}
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              className="px-3 py-1.5 rounded text-sm text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            onClick={() => setConfirmDelete(true)}
            disabled={cardCount > 0}
            className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
              cardCount > 0
                ? 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
                : 'bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90'
            }`}
          >
            Delete Project
          </button>
        )}
      </div>
    </div>
  );
}

interface ListEditorProps {
  label: string;
  items: string[];
  newValue: string;
  setNewValue: (v: string) => void;
  onAdd: () => void;
  onRemove: (v: string) => void;
  protectedItems?: string[];
  inputStyle: React.CSSProperties;
}

function ListEditor({ label, items, newValue, setNewValue, onAdd, onRemove, protectedItems, inputStyle }: ListEditorProps) {
  return (
    <div>
      <label className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>{label}</label>
      <div className="flex flex-wrap gap-1.5 mb-2">
        {items.map((item) => (
          <span
            key={item}
            className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded"
            style={{ backgroundColor: 'var(--bg2)', color: 'var(--fg)' }}
          >
            {item}
            {!(protectedItems || []).includes(item) && (
              <button
                onClick={() => onRemove(item)}
                className="hover:text-[var(--red)] transition-colors"
                style={{ color: 'var(--grey1)' }}
              >
                &times;
              </button>
            )}
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <input
          type="text"
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') onAdd(); }}
          placeholder={`Add ${label.toLowerCase().slice(0, -1)}...`}
          className="flex-1 px-3 py-1.5 rounded text-xs border focus:outline-none"
          style={inputStyle}
        />
        <button
          onClick={onAdd}
          disabled={!newValue.trim()}
          className="px-2 py-1.5 rounded text-xs transition-colors"
          style={{
            backgroundColor: newValue.trim() ? 'var(--bg3)' : 'var(--bg2)',
            color: newValue.trim() ? 'var(--fg)' : 'var(--grey1)',
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}
