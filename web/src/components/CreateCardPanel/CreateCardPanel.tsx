import { useCallback, useEffect, useId, useRef, useState } from 'react';
import type { Card, CreateCardInput, ProjectConfig } from '../../types';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { CardPanelBody, type RailTabKey } from '../CardPanel/CardPanelBody';
import { AutomationCheckboxes } from '../CardPanel/AutomationCheckboxes';
import { CardPanelEditor } from '../CardPanel/CardPanelEditor';
import { LabelsSection } from '../CardPanel/CardPanelLabels';
import { chipTint, typeColors, priorityColors, stateColors } from '../../lib/chip';
import { HeaderCaret, headerTitleStyle } from '../../lib/header-tokens';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ParentSearch } from './ParentSearch';

interface CreateCardPanelProps {
  config: ProjectConfig;
  cards: Card[];
  onClose: () => void;
  onCreate: (input: CreateCardInput, opts?: { run?: boolean; interactive?: boolean }) => Promise<void>;
}

/**
 * Bifold "new card" panel. Shares the visual shell with `CardPanel` (header
 * cluster + left-column form + right rail of tabs) so creation reads as the
 * same surface as editing. Differs in that:
 *   - The title is empty by default and editable inline.
 *   - The state chip is replaced by a single "new card" chip.
 *   - The Info tab swaps Status/Subtasks for an optional Parent picker.
 *   - The Automation tab drops the live run-status block (mode="create").
 *   - The header action cluster is Cancel · Just create · Create & Run.
 *
 * Behavior preserved from the previous CreateCardForm:
 *   - Type templates: switching type loads the matching template (with a
 *     dirty-body confirm).
 *   - Parent → subtask lock: setting a parent forces the type to "subtask"
 *     and restores the previous type when the parent is cleared.
 *   - XSS prevention: the description editor renders previews with
 *     `previewOptions={{ skipHtml: true }}` (handled by CardPanelEditor).
 *   - Server force-enable: clicking Create & Run sets feature_branch +
 *     create_pr to true on the input so client and server agree on the
 *     saved state (matches `internal/api/runner.go:runCard`).
 */
export function CreateCardPanel({ config, cards, onClose, onCreate }: CreateCardPanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const typeId = useId();
  const priorityId = useId();

  const [title, setTitle] = useState('');
  const [type, setType] = useState(config.types[0] || 'task');
  const [priority, setPriority] = useState(config.priorities[1] || config.priorities[0] || '');
  const [labels, setLabels] = useState<string[]>([]);
  const [parent, setParent] = useState('');
  const [body, setBody] = useState(() => config.templates?.[config.types[0]] ?? '');
  const [bodyDirty, setBodyDirty] = useState(false);
  const [autonomous, setAutonomous] = useState(false);
  const [featureBranch, setFeatureBranch] = useState(true);
  const [createPR, setCreatePR] = useState(false);
  const [baseBranch, setBaseBranch] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const isMobile = useMediaQuery('(max-width: 768px)');
  const [activeTab, setActiveTab] = useState<RailTabKey>(isMobile ? 'card' : 'automation');
  const [railExpanded, setRailExpanded] = useState(false);
  const [pendingTemplate, setPendingTemplate] = useState<{ type: string; body: string } | null>(null);

  useFocusTrap(panelRef, true);

  // Tracks the type the user had selected before a parent was set, so we
  // can restore it on clear. Updated by the wrapped `handleSetParent` setter
  // below — done synchronously alongside the parent change rather than via
  // an effect, to avoid the cascading-render lint and the corresponding
  // race where `type` could briefly read 'subtask' before the parent state
  // change settled.
  const prevTypeRef = useRef<string>(type);

  // Wrap setParent so the type-lock and parent change happen in one
  // commit, no effect required.
  const handleSetParent = useCallback((newParent: string) => {
    setParent(newParent);
    if (newParent) {
      if (type !== 'subtask') prevTypeRef.current = type;
      setType('subtask');
    } else {
      const restored = prevTypeRef.current === 'subtask' ? (config.types[0] ?? 'task') : prevTypeRef.current;
      setType(restored);
    }
  }, [type, config.types]);

  const { branches, loading: branchesLoading, error: branchesError } =
    useBranches(config.name, autonomous && !!config.remote_execution?.enabled);

  // Esc closes the panel.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleTypeChange = useCallback((newType: string) => {
    const template = config.templates?.[newType];
    if (template) {
      if (bodyDirty) {
        setPendingTemplate({ type: newType, body: template });
      } else {
        setBody(template);
      }
    } else if (!bodyDirty) {
      setBody('');
    }
    setType(newType);
  }, [config.templates, bodyDirty]);

  const buildInput = useCallback((forRun: boolean): CreateCardInput => ({
    title: title.trim(),
    type,
    priority,
    labels: labels.length > 0 ? labels : undefined,
    parent: parent || undefined,
    body: body || undefined,
    autonomous: autonomous || undefined,
    // Server force-enables both on Run; mirror that here so the persisted
    // record matches what the user sees in the form.
    feature_branch: forRun ? true : featureBranch || undefined,
    create_pr: forRun ? true : createPR || undefined,
    base_branch: baseBranch || undefined,
  }), [title, type, priority, labels, parent, body, autonomous, featureBranch, createPR, baseBranch]);

  const titleInputRef = useRef<HTMLInputElement>(null);

  const ensureTitle = useCallback((): boolean => {
    if (title.trim()) return true;
    titleInputRef.current?.focus();
    return false;
  }, [title]);

  const handleJustCreate = useCallback(async () => {
    if (isSubmitting) return;
    if (!ensureTitle()) return;
    setIsSubmitting(true);
    try {
      await onCreate(buildInput(false), { run: false });
    } catch {
      // Parent shows error toast; keep form open.
    } finally {
      setIsSubmitting(false);
    }
  }, [isSubmitting, ensureTitle, buildInput, onCreate]);

  const handleCreateAndRun = useCallback(async () => {
    if (isSubmitting) return;
    if (!ensureTitle()) return;
    setIsSubmitting(true);
    try {
      await onCreate(buildInput(true), { run: true, interactive: !autonomous });
    } catch {
      // Parent shows error toast; keep form open.
    } finally {
      setIsSubmitting(false);
    }
  }, [isSubmitting, ensureTitle, buildInput, onCreate, autonomous]);

  const typeTint = typeColors[type] || 'var(--grey1)';
  const priorityTint = priorityColors[priority] || 'var(--grey1)';
  const projectName = config.name;

  // Left column — labels + description (always editable in create mode).
  const left = (
    <>
      <LabelsSection
        editedLabels={labels}
        disabled={false}
        onLabelsChange={setLabels}
      />
      <CardPanelEditor
        body={body}
        editable
        editing
        onChange={(v) => { setBody(v); setBodyDirty(true); }}
      />
    </>
  );

  // Right rail — Automation + Info. No Chat (no runner) and no Danger Zone
  // (the card doesn't exist yet, so there's nothing destructive to do).
  const tabs = [
    {
      key: 'automation' as RailTabKey,
      label: 'Automation',
      content: (
        <div className="bf-auto-top" style={{ maxHeight: 'none' }}>
          <AutomationCheckboxes
            mode="create"
            autonomous={autonomous}
            featureBranch={featureBranch}
            createPR={createPR}
            onAutonomousChange={setAutonomous}
            onFeatureBranchChange={(v) => {
              setFeatureBranch(v);
              if (!v) setCreatePR(false);
            }}
            onCreatePRChange={setCreatePR}
            baseBranch={baseBranch}
            onBaseBranchChange={setBaseBranch}
            branches={branches}
            branchesLoading={branchesLoading}
            branchesError={branchesError}
          />
        </div>
      ),
    },
    {
      key: 'info' as RailTabKey,
      label: 'Info',
      content: (
        <div className="flex-1 min-h-0 overflow-y-auto">
          <section className="bf-aside-section">
            <h4>Agent</h4>
            <div className="bf-spread">
              <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>no agent yet</span>
              <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>assigned on create</span>
            </div>
          </section>

          <section className="bf-aside-section">
            <h4>Parent (optional)</h4>
            <ParentSearch parent={parent} setParent={handleSetParent} cards={cards} />
            <div className="font-mono mt-2" style={{ color: 'var(--grey1)', fontSize: '11px', lineHeight: 1.45 }}>
              Leave empty for a top-level card. Setting a parent locks the type to <code style={{ color: 'var(--purple)' }}>subtask</code>.
            </div>
          </section>

          <section className="bf-aside-section">
            <h4>Initial state</h4>
            <div className="text-xs flex items-center gap-2" style={{ color: 'var(--grey1)' }}>
              <span>Cards are created in</span>
              <span className="chip-pill" style={chipTint(stateColors[config.states[0]] || 'var(--grey1)')}>
                {config.states[0]}
              </span>
            </div>
          </section>
        </div>
      ),
    },
  ];

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />

      <div
        ref={panelRef}
        className="card-panel card-panel-bifold animate-panel-slide-in"
        role="dialog"
        aria-modal="true"
        aria-label="Create card"
      >
        {/* Header */}
        <div className="flex flex-wrap items-start gap-x-4 gap-y-3 px-5 py-4 border-b border-[var(--bg3)]">
          <div className="flex-1 min-w-0 flex flex-col gap-2" style={{ flexBasis: '340px' }}>
            <div className="flex items-center gap-2 flex-wrap">
              <button
                onClick={onClose}
                className="text-[var(--grey1)] hover:text-[var(--fg)] transition-colors shrink-0"
                title="Cancel (Esc)"
                aria-label="Close panel"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>

              <span className="chip-pill chip-state-todo">new card</span>

              {/* Type picker chip */}
              <label htmlFor={typeId} className="sr-only">Type</label>
              {parent ? (
                <span className="chip-pill" style={chipTint('var(--aqua)')}>
                  subtask
                  <span className="text-[10px] opacity-70 ml-1">(set by parent)</span>
                </span>
              ) : (
                <div
                  className="chip-pill"
                  style={{ ...chipTint(typeTint), padding: '3px 4px 3px 8px', gap: '2px' }}
                >
                  <select
                    id={typeId}
                    value={type}
                    onChange={(e) => handleTypeChange(e.target.value)}
                    className="bg-transparent border-none outline-none appearance-none"
                    style={{
                      color: typeTint,
                      fontFamily: 'var(--font-mono)',
                      fontSize: '11px',
                      letterSpacing: '0.02em',
                      paddingRight: '14px',
                      marginRight: '-12px',
                    }}
                    aria-label="Type"
                  >
                    {config.types.filter((t) => t !== 'subtask').map((t) => (
                      <option key={t} value={t} className="bg-[var(--bg2)] text-[var(--fg)]">{t}</option>
                    ))}
                  </select>
                  <span className="pointer-events-none">{HeaderCaret}</span>
                </div>
              )}

              {/* Priority picker chip */}
              <label htmlFor={priorityId} className="sr-only">Priority</label>
              <div
                className="chip-pill"
                style={{ ...chipTint(priorityTint), padding: '3px 4px 3px 8px', gap: '2px' }}
              >
                <select
                  id={priorityId}
                  value={priority}
                  onChange={(e) => setPriority(e.target.value)}
                  className="bg-transparent border-none outline-none appearance-none"
                  style={{
                    color: priorityTint,
                    fontFamily: 'var(--font-mono)',
                    fontSize: '11px',
                    letterSpacing: '0.02em',
                    paddingRight: '14px',
                    marginRight: '-12px',
                  }}
                  aria-label="Priority"
                >
                  {config.priorities.map((p) => (
                    <option key={p} value={p} className="bg-[var(--bg2)] text-[var(--fg)]">{p}</option>
                  ))}
                </select>
                <span className="pointer-events-none">{HeaderCaret}</span>
              </div>

              {projectName && (
                <span className="font-mono text-[11px] text-[var(--grey0)]">{projectName}</span>
              )}
            </div>

            <label htmlFor={titleId} className="sr-only">Title</label>
            <input
              id={titleId}
              ref={titleInputRef}
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
              className="w-full bg-transparent text-[var(--fg)] focus:outline-none focus:bg-[var(--bg2)] rounded px-1 -mx-1 border border-transparent focus:border-[var(--bg3)]"
              style={headerTitleStyle}
              placeholder="Card title — one sentence, imperative ideally"
            />
          </div>

          <div className="flex items-center gap-2 ml-auto shrink-0 flex-wrap justify-end">
            <button
              type="button"
              onClick={onClose}
              className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => void handleJustCreate()}
              disabled={isSubmitting}
              className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs disabled:opacity-50"
              title={title.trim() ? 'Create without running' : 'Add a title first'}
            >
              Just create
            </button>
            <button
              type="button"
              onClick={() => void handleCreateAndRun()}
              disabled={isSubmitting}
              className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-colors text-sm font-medium inline-flex items-center gap-2 disabled:opacity-60"
              title={title.trim() ? 'Create and immediately run' : 'Add a title first'}
            >
              <span aria-hidden="true">▶</span>
              <span>{isSubmitting ? 'Creating…' : `Create & Run ${autonomous ? 'Auto' : 'HITL'}`}</span>
            </button>
          </div>
        </div>

        <CardPanelBody
          left={left}
          tabs={tabs}
          activeTab={activeTab}
          onTabChange={setActiveTab}
          railExpanded={railExpanded}
          onToggleRail={() => setRailExpanded((v) => !v)}
        />
      </div>

      <ConfirmModal
        open={pendingTemplate !== null}
        title={pendingTemplate ? `Load template for "${pendingTemplate.type}"?` : ''}
        message="This will replace the current body."
        confirmLabel="Load template"
        onConfirm={() => {
          if (pendingTemplate) {
            setBody(pendingTemplate.body);
            setBodyDirty(false);
          }
          setPendingTemplate(null);
        }}
        onCancel={() => setPendingTemplate(null)}
      />
    </>
  );
}
