import { useState, useCallback, useMemo, useEffect, useId } from 'react';
import { api, isAPIError } from '../../api/client';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useTheme } from '../../hooks/useTheme';
import type { GitHubImportConfig, ProjectConfig, UpdateProjectInput } from '../../types';
import { CardDefaultsSection } from './CardDefaultsSection';
import {
  cardDefaultsKey,
  resolveCardDefaults,
  toWireCardDefaults,
  type ResolvedCardDefaults,
} from '../../lib/cardDefaults';
import { DangerSection } from './DangerSection';
import { DefaultSkillsSelector } from './DefaultSkillsSelector';
import { GitHubCredentialSection } from './GitHubCredentialSection';
import { GitHubImportSection } from './GitHubImportSection';
import { RepoListSection } from './RepoListSection';
import { SettingsHeader } from './SettingsHeader';
import { SettingsSection } from './SettingsSection';
import { SettingsTabs, type SettingsTab } from './SettingsTabs';
import { settingsPanelId, settingsTabId, type SettingsTabKey } from './settingsTabIds';
import { StateTransitionEditor } from './StateTransitionEditor';
import { RemoteExecutionSection } from './RemoteExecutionSection';
import type { RemoteExecutionConfig } from './RemoteExecutionSection';
import { VerifySection } from './VerifySection';
import type { VerifyConfig } from './VerifySection';

interface ProjectSettingsProps {
  project: string;
  onUpdated: (config: ProjectConfig) => void;
  onDeleted: () => void;
  showToast: (message: string, type: 'success' | 'error' | 'info') => void;
}

const emptyGitHub: GitHubImportConfig = { import_issues: false };
const emptyRemoteExecution: RemoteExecutionConfig = {};
const emptyVerify: VerifyConfig = {};

function ghToString(gh: GitHubImportConfig | undefined): string {
  return JSON.stringify(gh ?? emptyGitHub);
}

// verifyToString normalizes a verify config to a stable shape so the diff-guard
// treats absent and zero-value fields identically. remote_execution uses a
// touched flag instead (see handleSave).
function verifyToString(v: VerifyConfig | undefined): string {
  const vv = v ?? emptyVerify;
  return JSON.stringify({
    command: vv.command ?? '',
    timeout_seconds: vv.timeout_seconds ?? 0,
    env: vv.env ?? [],
  });
}

/**
 * Serialise a `Record<string, string[]>` with sorted keys so that the
 * comparison is deterministic regardless of insertion order. Without
 * sorting, `removeItem` rebuilds the map from `Object.keys(...)` which
 * may reorder keys and produce a false-positive dirty signal.
 */
function serializeTransitions(t: Record<string, string[]>): string {
  return JSON.stringify(
    Object.fromEntries(
      Object.keys(t)
        .sort()
        .map((k) => [k, [...t[k]].sort()]),
    ),
  );
}

export function ProjectSettings({ project, onUpdated, onDeleted, showToast }: ProjectSettingsProps) {
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<SettingsTabKey>('source');

  // Mirrors UserMenu/Sidebar's useOptionalAuth pattern - mode defaults to
  // 'none' and isAdmin to false when rendered without an AuthProvider (e.g.
  // in isolated tests), matching none-mode behavior.
  const auth = useOptionalAuth();
  const mode = auth?.mode ?? 'none';
  const isAdmin = Boolean(auth?.user?.is_admin);
  const readOnly = mode === 'multi' && !isAdmin;

  const {
    chatEnabled, taskBackend, mobMaxParticipants, mobDefaultParticipants, mobExecuteCheckpoints, boardsRepos = [],
  } = useTheme();
  // Mirrors the sidebar: the boards repo only means something when the
  // instance serves more than one.
  const multiRepo = boardsRepos.length > 1;

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
  // Tracks whether the operator interacted with the Remote Execution section
  // this session - the payload signal for remote_execution: untouched saves
  // omit the key so the server preserves the stored config.
  const [remoteExecutionTouched, setRemoteExecutionTouched] = useState(false);
  const [verify, setVerify] = useState<VerifyConfig>(emptyVerify);
  const [cardDefaults, setCardDefaults] = useState<ResolvedCardDefaults>(() => resolveCardDefaults());
  const [defaultSkills, setDefaultSkills] = useState<string[] | null>(null);
  const [githubCredential, setGithubCredential] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [cardCount, setCardCount] = useState(0);

  // Reset loading/error on project change (render-time pattern).
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setLoading(true);
    setError(null);
    setActiveTab('source');
  }

  // applyConfig seeds every form field from a (transition-normalized) config.
  // Used on load and by Discard, so both land on the identical baseline.
  const applyConfig = useCallback((cfg: ProjectConfig) => {
    setRepo(cfg.repo || '');
    setStates(cfg.states);
    setTypes(cfg.types);
    setPriorities(cfg.priorities);
    setTransitions(cfg.transitions);
    setNewState('');
    setNewType('');
    setNewPriority('');
    setGitHub(cfg.github ?? emptyGitHub);
    setRemoteExecution(cfg.remote_execution ?? emptyRemoteExecution);
    setRemoteExecutionTouched(false);
    setVerify(cfg.verify ?? emptyVerify);
    setCardDefaults(resolveCardDefaults(cfg.card_defaults));
    setDefaultSkills(cfg.default_skills ?? null);
    setGithubCredential(cfg.github_credential ?? '');
  }, []);

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
        applyConfig(normalizedConfig);
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
  }, [project, applyConfig]);

  // Per-section dirty flags feed the tab dots, the Save button and the
  // payload guards in handleSave, so a dotted tab always means "this save
  // will carry that section".
  const dirty = useMemo(() => {
    if (!config) return null;
    return {
      repo: repo !== (config.repo || ''),
      credential: githubCredential !== (config.github_credential ?? ''),
      import: ghToString(github) !== ghToString(config.github),
      lists:
        JSON.stringify(states) !== JSON.stringify(config.states) ||
        JSON.stringify(types) !== JSON.stringify(config.types) ||
        JSON.stringify(priorities) !== JSON.stringify(config.priorities),
      transitions: serializeTransitions(transitions) !== serializeTransitions(config.transitions),
      cardDefaults:
        cardDefaultsKey(cardDefaults) !== cardDefaultsKey(resolveCardDefaults(config.card_defaults)),
      skills: JSON.stringify(defaultSkills) !== JSON.stringify(config.default_skills ?? null),
      images: remoteExecutionTouched,
      verify: verifyToString(verify) !== verifyToString(config.verify),
    };
  }, [
    config,
    repo,
    states,
    types,
    priorities,
    transitions,
    github,
    remoteExecutionTouched,
    verify,
    defaultSkills,
    githubCredential,
    cardDefaults,
  ]);

  const isDirty = dirty !== null && Object.values(dirty).some(Boolean);

  // Save disables itself and Discard unmounts once the edits are gone, so
  // keyboard focus would otherwise drop to the body.
  const focusActiveTab = useCallback(() => {
    document.getElementById(settingsTabId(activeTab))?.focus();
  }, [activeTab]);

  const tabs: SettingsTab[] = [
    { key: 'source', label: 'Source', dirty: !!dirty && (dirty.repo || dirty.credential || dirty.import) },
    { key: 'workflow', label: 'Workflow', dirty: !!dirty && (dirty.lists || dirty.transitions) },
    { key: 'automation', label: 'Automation', dirty: !!dirty && (dirty.cardDefaults || dirty.skills) },
    { key: 'execution', label: 'Execution', dirty: !!dirty && (dirty.images || dirty.verify) },
    { key: 'danger', label: 'Danger', danger: true, dirty: false },
  ];

  const handleSave = useCallback(async () => {
    if (!dirty || !isDirty || isSaving) return;
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
        // Multi-mode only - omitting the key entirely in none mode keeps
        // the request body byte-identical to pre-binding behavior (none
        // mode also rejects a non-empty binding server-side, fail-closed).
        //
        // Within multi mode, only include the key when the value actually
        // changed from the loaded config. UpdateProjectInput.github_credential
        // has pointer semantics server-side: omitted = preserve, "" = clear,
        // name = set (re-validated against the pool, 422 if unknown). Always
        // sending it would force server-side re-validation on every save,
        // including saves that don't touch the binding at all - which fails
        // with 422 whenever the current binding has gone stale (its pool
        // entry was deleted), exactly the scenario GitHubCredentialSection's
        // warning exists for. Comparing against the loaded config preserves
        // "save without touching it" even for a stale binding.
        ...(mode === 'multi' && dirty.credential ? { github_credential: githubCredential } : {}),
        // Send remote_execution only when the operator actually touched the
        // section this session; the server merges field-by-field and
        // preserves the stored config when the key is omitted. Both images
        // are always sent as strings ("" clears the override).
        ...(dirty.images
          ? {
              remote_execution: {
                worker_image: remoteExecution.worker_image ?? '',
                chat_worker_image: remoteExecution.chat_worker_image ?? '',
              },
            }
          : {}),
        // Only send verify when it changed from the loaded config. The server
        // replaces the whole struct and normalizes a zero-value object to nil,
        // so clearing every field here clears the project's verify config; an
        // untouched save omits the key and preserves it. env is included only
        // when it has names: a project has nothing to inherit, so an empty env
        // is "no env" - omitting the key keeps `env: []` out of .board.yaml
        // (the server preserves a non-nil empty env for the card-override path,
        // which this project-level form never sends).
        ...(dirty.verify
          ? {
              verify: {
                command: verify.command ?? '',
                timeout_seconds: verify.timeout_seconds ?? 0,
                ...(verify.env && verify.env.length > 0 ? { env: verify.env } : {}),
              },
            }
          : {}),
        // Send card_defaults only when it changed from the loaded config; the
        // server replaces the whole block and normalizes built-ins to nil, so
        // resetting every row to the built-ins clears it from .board.yaml.
        ...(dirty.cardDefaults ? { card_defaults: toWireCardDefaults(cardDefaults) } : {}),
      };
      const updated = await api.updateProject(project, input);
      setConfig(updated);
      // The save landed; the section state is now the baseline.
      setRemoteExecutionTouched(false);
      onUpdated(updated);
      showToast('Project settings saved', 'success');
      focusActiveTab();
    } catch (err) {
      const errMsg = isAPIError(err)
        ? (err.details ? `${err.error}: ${err.details}` : err.error)
        : 'Failed to save';
      showToast(errMsg, 'error');
    } finally {
      setIsSaving(false);
    }
  }, [
    dirty,
    isDirty,
    isSaving,
    repo,
    states,
    types,
    priorities,
    transitions,
    github,
    remoteExecution,
    verify,
    defaultSkills,
    githubCredential,
    cardDefaults,
    mode,
    project,
    onUpdated,
    showToast,
    focusActiveTab,
  ]);

  const handleDiscard = useCallback(() => {
    if (config) applyConfig(config);
    focusActiveTab();
  }, [config, applyConfig, focusActiveTab]);

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

  // Every panel stays mounted and inactive ones are hidden (the ARIA tabs
  // pattern). Sections keep their local state across tab switches - the
  // pending "Constrain to selected skills" choice, the raw env text - and
  // their fetches run once per page load, as they did before the tabs.
  const renderTab = (key: SettingsTabKey) => {
    switch (key) {
      case 'source':
        return (
          <>
            <SettingsSection title="Repository">
              <div className="ps-field">
                <label htmlFor={repoId} className="ps-label">Repository URL</label>
                <input
                  id={repoId}
                  type="text"
                  value={repo}
                  onChange={(e) => setRepo(e.target.value)}
                  placeholder="https://github.com/org/repo.git"
                  className="bf-input"
                />
              </div>

              {/*
                GitHub credential binding - multi-user mode only. Bindings are
                a multi-mode feature (the API rejects a non-empty binding in
                none mode), so the row is not rendered at all in none mode;
                that keeps none-mode settings byte-identical to pre-binding
                behavior.
              */}
              {mode === 'multi' && (
                <div className="mt-3">
                  <GitHubCredentialSection value={githubCredential} onChange={setGithubCredential} readOnly={readOnly} />
                </div>
              )}
            </SettingsSection>

            <SettingsSection title="GitHub issue import">
              <GitHubImportSection github={github} onChange={setGitHub} types={types} priorities={priorities} />
            </SettingsSection>
          </>
        );
      case 'workflow':
        return (
          <>
            <SettingsSection title="States, types, priorities">
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
              />
            </SettingsSection>

            <SettingsSection
              title="Transitions"
              lead="Which states a card may move to. Rows are where a card is, columns are where it can go."
            >
              <StateTransitionEditor states={states} transitions={transitions} onChange={setTransitions} />
            </SettingsSection>
          </>
        );
      case 'automation':
        return (
          <>
            {/* Card defaults - what a new card's Automation rail starts with */}
            <SettingsSection title="Card defaults">
              <CardDefaultsSection
                value={cardDefaults}
                onChange={setCardDefaults}
                taskBackend={taskBackend}
                mobMaxParticipants={mobMaxParticipants}
                mobDefaultParticipants={mobDefaultParticipants}
                mobExecuteCheckpoints={mobExecuteCheckpoints}
              />
            </SettingsSection>

            <SettingsSection title="Default task skills">
              <DefaultSkillsSelector value={defaultSkills} onChange={setDefaultSkills} />
            </SettingsSection>
          </>
        );
      case 'execution':
        return (
          <>
            <SettingsSection title="Remote execution">
              <RemoteExecutionSection
                value={remoteExecution}
                onChange={(next) => {
                  setRemoteExecution(next);
                  setRemoteExecutionTouched(true);
                }}
                readOnly={readOnly}
                taskBackendConfigured={!!taskBackend}
                chatEnabled={chatEnabled}
              />
            </SettingsSection>

            <SettingsSection title="Verify">
              <VerifySection value={verify} onChange={setVerify} />
            </SettingsSection>
          </>
        );
      case 'danger':
        return (
          <DangerSection
            project={project}
            cardCount={cardCount}
            isDeleting={isDeleting}
            onDelete={handleDelete}
          />
        );
    }
  };

  return (
    <div className="ps-page">
      <div className="ps-col">
        <SettingsHeader
          config={config}
          cardCount={cardCount}
          boardsRepo={multiRepo ? config.boards_repo : undefined}
          readOnly={readOnly}
          isDirty={isDirty}
          isSaving={isSaving}
          onSave={handleSave}
          onDiscard={handleDiscard}
        />

        <SettingsTabs tabs={tabs} active={activeTab} onChange={setActiveTab} />

        {/*
          readOnly (non-admin, multi mode) freezes every control below via
          native fieldset[disabled] propagation to descendant form elements
          (input/select/button/textarea) - every editable control here is a
          native form element, so this covers them without per-section changes.
          `contents` removes the fieldset's own box so it doesn't affect layout.
          The tab strip sits outside the fieldset so read-only viewers can
          still move between tabs.
        */}
        <fieldset disabled={readOnly} className="contents">
          {tabs.map((t) => (
            <div
              key={t.key}
              id={settingsPanelId(t.key)}
              role="tabpanel"
              tabIndex={0}
              aria-labelledby={settingsTabId(t.key)}
              hidden={t.key !== activeTab}
            >
              {renderTab(t.key)}
            </div>
          ))}
        </fieldset>
      </div>
    </div>
  );
}
