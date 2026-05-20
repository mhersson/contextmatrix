import { useState, useCallback, useMemo, useEffect, useId } from 'react';
import { api, isAPIError } from '../../api/client';
import type { GitHubImportConfig, ProjectConfig, UpdateProjectInput } from '../../types';
import { DefaultSkillsSelector } from './DefaultSkillsSelector';
import { GitHubImportSection } from './GitHubImportSection';
import { RepoListSection } from './RepoListSection';
import { StateTransitionEditor } from './StateTransitionEditor';
import { RemoteExecutionSection } from './RemoteExecutionSection';
import type { RemoteExecutionConfig } from './RemoteExecutionSection';

interface ProjectSettingsProps {
  project: string;
  onUpdated: (config: ProjectConfig) => void;
  onDeleted: () => void;
  showToast: (message: string, type: 'success' | 'error' | 'info') => void;
}

const emptyGitHub: GitHubImportConfig = { import_issues: false };
const emptyRemoteExecution: RemoteExecutionConfig = {};

function ghToString(gh: GitHubImportConfig | undefined): string {
  return JSON.stringify(gh ?? emptyGitHub);
}

function reToString(re: RemoteExecutionConfig | undefined): string {
  return JSON.stringify(re ?? emptyRemoteExecution);
}

export function ProjectSettings({ project, onUpdated, onDeleted, showToast }: ProjectSettingsProps) {
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const repoId = useId();

  const [repo, setRepo] = useState('');
  const [states, setStates] = useState<string[]>([]);
  const [types, setTypes] = useState<string[]>([]);
  const [priorities, setPriorities] = useState<string[]>([]);
  const [transitions, setTransitions] = useState<Record<string, string[]>>({});
  const [newState, setNewState] = useState('');
  const [newType, setNewType] = useState('');
  const [newPriority, setNewPriority] = useState('');
  const [github, setGitHub] = useState<GitHubImportConfig>(emptyGitHub);
  const [remoteExecution, setRemoteExecution] = useState<RemoteExecutionConfig>(emptyRemoteExecution);
  const [defaultSkills, setDefaultSkills] = useState<string[] | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [cardCount, setCardCount] = useState(0);

  // Reset loading/error on project change (render-time pattern).
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setLoading(true);
    setError(null);
  }

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      api.getProject(project),
      api.getCards(project).then(cards => cards.length),
    ])
      .then(([cfg, count]) => {
        if (cancelled) return;
        // Normalize transitions: ensure all states have an entry (even if empty)
        // This prevents isDirty from being true immediately after load
        const normalizedTransitions: Record<string, string[]> = { ...cfg.transitions };
        cfg.states.forEach(s => {
          if (!(s in normalizedTransitions)) normalizedTransitions[s] = [];
        });
        const normalizedConfig = { ...cfg, transitions: normalizedTransitions };
        setConfig(normalizedConfig);
        setRepo(cfg.repo || '');
        setStates(cfg.states);
        setTypes(cfg.types);
        setPriorities(cfg.priorities);
        setTransitions(normalizedTransitions);
        setGitHub(cfg.github ?? emptyGitHub);
        setRemoteExecution(cfg.remote_execution ?? emptyRemoteExecution);
        setDefaultSkills(cfg.default_skills ?? null);
        setCardCount(count);
        setLoading(false);
      })
      .catch(err => {
        if (cancelled) return;
        setError(isAPIError(err) ? err.error : 'Failed to load project');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [project]);

  /**
   * Serialise a `Record<string, string[]>` with sorted keys so that the
   * comparison is deterministic regardless of insertion order. Without
   * sorting, `removeItem` rebuilds the map from `Object.keys(...)` which
   * may reorder keys and produce a false-positive dirty signal.
   */
  const serializeTransitions = useCallback(
    (t: Record<string, string[]>): string =>
      JSON.stringify(
        Object.fromEntries(
          Object.keys(t)
            .sort()
            .map((k) => [k, [...t[k]].sort()]),
        ),
      ),
    [],
  );

  const isDirty = useMemo(() => {
    if (!config) return false;
    const configDefaultSkills = config.default_skills ?? null;
    return (
      repo !== (config.repo || '') ||
      JSON.stringify(states) !== JSON.stringify(config.states) ||
      JSON.stringify(types) !== JSON.stringify(config.types) ||
      JSON.stringify(priorities) !== JSON.stringify(config.priorities) ||
      serializeTransitions(transitions) !== serializeTransitions(config.transitions) ||
      ghToString(github) !== ghToString(config.github) ||
      reToString(remoteExecution) !== reToString(config.remote_execution) ||
      JSON.stringify(defaultSkills) !== JSON.stringify(configDefaultSkills)
    );
  }, [config, repo, states, types, priorities, transitions, github, remoteExecution, defaultSkills, serializeTransitions]);

  const handleSave = useCallback(async () => {
    if (!isDirty || isSaving) return;
    setIsSaving(true);
    try {
      const input: UpdateProjectInput = {
        repo: repo || undefined,
        states,
        types,
        priorities,
        transitions,
        github: github.import_issues ? github : { import_issues: false },
        default_skills: defaultSkills,
      };
      const updated = await api.updateProject(project, input);
      setConfig(updated);
      onUpdated(updated);
      showToast('Project settings saved', 'success');
    } catch (err) {
      const errMsg = isAPIError(err)
        ? (err.details ? `${err.error}: ${err.details}` : err.error)
        : 'Failed to save';
      showToast(errMsg, 'error');
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, repo, states, types, priorities, transitions, github, defaultSkills, project, onUpdated, showToast]);

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

  const removeState = useCallback((value: string) => {
    setStates(prev => prev.filter(v => v !== value));
    setTransitions(prev => {
      const next = { ...prev };
      delete next[value];
      for (const key of Object.keys(next)) {
        next[key] = next[key].filter(s => s !== value);
      }
      return next;
    });
  }, []);

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
        <h2
          style={{
            color: 'var(--fg)',
            fontFamily: 'var(--font-display)',
            fontWeight: 500,
            fontSize: '24px',
            letterSpacing: '-0.015em',
            lineHeight: 1.2,
          }}
        >
          Project Settings
        </h2>
        <button
          onClick={handleSave}
          disabled={!isDirty || isSaving}
          className={`px-4 py-1.5 rounded text-sm font-medium transition-colors ${
            isDirty
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
          <div className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Name</div>
          <div className="px-3 py-2 rounded text-sm" style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}>
            {config.name}
          </div>
        </div>
        <div>
          <div className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Prefix</div>
          <div className="px-3 py-2 rounded text-sm" style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}>
            {config.prefix}
          </div>
        </div>
      </div>

      {/* Repo */}
      <div>
        <label htmlFor={repoId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>Repository URL</label>
        <input
          id={repoId}
          type="text"
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          placeholder="git@github.com:org/repo.git"
          className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
          style={inputStyle}
        />
      </div>

      {/* States / Types / Priorities */}
      <RepoListSection
        states={states}
        newState={newState}
        setNewState={setNewState}
        onAddState={() => {
          const trimmed = newState.trim();
          if (trimmed && !states.includes(trimmed)) {
            setStates(prev => [...prev, trimmed]);
            setTransitions(prev => (trimmed in prev ? prev : { ...prev, [trimmed]: [] }));
            setNewState('');
          }
        }}
        onRemoveState={removeState}
        types={types}
        newType={newType}
        setNewType={setNewType}
        onAddType={() => {
          const trimmed = newType.trim();
          if (trimmed && !types.includes(trimmed)) {
            setTypes(prev => [...prev, trimmed]);
            setNewType('');
          }
        }}
        onRemoveType={(v) => setTypes(prev => prev.filter(x => x !== v))}
        priorities={priorities}
        newPriority={newPriority}
        setNewPriority={setNewPriority}
        onAddPriority={() => {
          const trimmed = newPriority.trim();
          if (trimmed && !priorities.includes(trimmed)) {
            setPriorities(prev => [...prev, trimmed]);
            setNewPriority('');
          }
        }}
        onRemovePriority={(v) => setPriorities(prev => prev.filter(x => x !== v))}
        inputStyle={inputStyle}
      />

      {/* State transition matrix */}
      <StateTransitionEditor
        states={states}
        transitions={transitions}
        onChange={setTransitions}
        inputStyle={inputStyle}
      />

      {/* Default task skills */}
      <DefaultSkillsSelector value={defaultSkills} onChange={setDefaultSkills} />

      {/* Remote Execution */}
      <RemoteExecutionSection
        value={remoteExecution}
        onChange={setRemoteExecution}
        inputStyle={inputStyle}
      />

      {/* GitHub Issue Import */}
      <GitHubImportSection
        github={github}
        onChange={setGitHub}
        types={types}
        priorities={priorities}
        inputStyle={inputStyle}
      />

      {/* Danger zone */}
      <div className="pt-4 border-t" style={{ borderColor: 'var(--bg3)' }}>
        <h3 className="section-eyebrow mb-2" style={{ color: 'var(--red)' }}>Danger Zone</h3>
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

