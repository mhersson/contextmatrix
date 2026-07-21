import { useEffect, useId, useRef, useState } from 'react';
import type { Card, CreateCardInput, ProjectConfig } from '../../types';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import { useTheme } from '../../hooks/useTheme';
import { useModelCatalog } from '../../hooks/useModelCatalog';
import { CardPanelBody, type RailTabKey } from '../CardPanel/CardPanelBody';
import { AutomationCheckboxes } from '../CardPanel/AutomationCheckboxes';
import type { ModelPinField } from '../CardPanel/ModelPinsSection';
import { CardPanelEditor } from '../CardPanel/CardPanelEditor';
import { LabelsSection } from '../CardPanel/CardPanelLabels';
import { MetadataSkills } from '../CardPanel/metadata/MetadataSkills';
import { chipTint, typeColors, priorityColors, stateColors } from '../../lib/chip';
import { headerTitleStyle } from '../../lib/header-tokens';
import { BifoldHeader } from '../CardPanel/BifoldHeader';
import { ChipPicker } from '../CardPanel/ChipPicker';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ParentSearch } from './ParentSearch';
import { useCreateCardForm } from './useCreateCardForm';

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
 *   - create_pr is always sent as an explicit boolean: the server defaults
 *     an absent value to true at create, so an unchecked box must reach it
 *     as false, never as an omission.
 */
export function CreateCardPanel({ config, cards, onClose, onCreate }: CreateCardPanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const typeId = useId();
  const priorityId = useId();

  const { form, titleInputRef } = useCreateCardForm(config, onCreate);

  const {
    taskBackend,
    bestOfNMax,
    bestOfNDefault,
    mobMaxParticipants,
    mobDefaultParticipants,
    mobGuestNames,
    mobExecuteCheckpoints,
  } = useTheme();
  // Card model pins: CM's served catalog (GET /api/models) - the vendor-
  // screened OpenRouter list or the endpoint list. Agent path only.
  const catalog = useModelCatalog(taskBackend === 'agent');
  const models = catalog.models.map((m) => m.id);

  // Field-keyed dispatch - a future ModelPinField union extension fails the
  // Record exhaustiveness check at compile time instead of silently routing
  // to the wrong setter.
  const pinSetters: Record<ModelPinField, (v: string) => void> = {
    model_orchestrator: form.setModelOrchestrator,
    model_coder: form.setModelCoder,
    model_reviewer: form.setModelReviewer,
  };

  const isMobile = useMediaQuery('(max-width: 768px)');
  const [activeTab, setActiveTab] = useState<RailTabKey>(isMobile ? 'card' : 'automation');
  const [railExpanded, setRailExpanded] = useState(false);

  useFocusTrap(panelRef, true);

  const { branches, loading: branchesLoading, error: branchesError } =
    useBranches(config.name, !!taskBackend);

  // Esc closes the panel.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const typeTint = typeColors[form.type] || 'var(--grey1)';
  const priorityTint = priorityColors[form.priority] || 'var(--grey1)';
  const projectName = config.name;

  // Left column - labels + description (always editable in create mode).
  const left = (
    <>
      <LabelsSection
        editedLabels={form.labels}
        disabled={false}
        onLabelsChange={form.setLabels}
      />
      <CardPanelEditor
        body={form.body}
        editable
        editing
        onChange={(v) => { form.setBody(v); form.setBodyDirty(true); }}
      />
    </>
  );

  // Right rail - Automation + Info. No Chat (no worker) and no Danger Zone
  // (the card doesn't exist yet, so there's nothing destructive to do).
  const tabs = [
    {
      key: 'automation' as RailTabKey,
      label: 'Automation',
      content: (
        <div className="bf-auto-top" style={{ maxHeight: 'none' }}>
          <AutomationCheckboxes
            mode="create"
            autonomous={form.autonomous}
            createPR={form.createPR}
            taskBackend={taskBackend}
            modelOrchestrator={form.modelOrchestrator}
            modelCoder={form.modelCoder}
            modelReviewer={form.modelReviewer}
            onModelPinChange={(field, value) => pinSetters[field](value)}
            models={models}
            onAutonomousChange={form.setAutonomous}
            onCreatePRChange={form.setCreatePR}
            bestOfN={form.bestOfN}
            bestOfNMax={bestOfNMax}
            bestOfNDefault={bestOfNDefault}
            onBestOfNChange={form.setBestOfN}
            mobParticipants={form.mobParticipants}
            mobMaxParticipants={mobMaxParticipants}
            mobDefaultParticipants={mobDefaultParticipants}
            mobPhases={form.mobPhases}
            onMobParticipantsChange={form.setMobParticipants}
            onMobPhasesChange={form.setMobPhases}
            mobGuests={form.mobGuests}
            mobGuestNames={mobGuestNames}
            mobExecuteCheckpoints={mobExecuteCheckpoints}
            onMobGuestsChange={form.setMobGuests}
            baseBranch={form.baseBranch}
            onBaseBranchChange={form.setBaseBranch}
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
            <ParentSearch parent={form.parent} setParent={form.handleSetParent} cards={cards} />
            <div className="font-mono mt-2" style={{ color: 'var(--grey1)', fontSize: '11px', lineHeight: 1.45 }}>
              Leave empty for a top-level card. Setting a parent locks the type to <code style={{ color: 'var(--purple)' }}>subtask</code>.
            </div>
          </section>

          <MetadataSkills
            value={form.skills}
            config={config}
            onSkillsChange={form.setSkills}
          />

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
        <BifoldHeader
          chips={
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
              {form.parent ? (
                <span className="chip-pill" style={chipTint('var(--aqua)')}>
                  subtask
                  <span className="text-[10px] opacity-70 ml-1">(set by parent)</span>
                </span>
              ) : (
                <ChipPicker
                  id={typeId}
                  value={form.type}
                  options={config.types.filter((t) => t !== 'subtask')}
                  tint={typeTint}
                  ariaLabel="Type"
                  onChange={form.handleTypeChange}
                />
              )}

              {/* Priority picker chip */}
              <label htmlFor={priorityId} className="sr-only">Priority</label>
              <ChipPicker
                id={priorityId}
                value={form.priority}
                options={config.priorities}
                tint={priorityTint}
                ariaLabel="Priority"
                onChange={form.setPriority}
              />

              {projectName && (
                <span className="font-mono text-[11px] text-[var(--grey0)]">{projectName}</span>
              )}
            </div>
          }
          title={
            <>
              <label htmlFor={titleId} className="sr-only">Title</label>
              <input
                id={titleId}
                ref={titleInputRef}
                type="text"
                value={form.title}
                onChange={(e) => form.setTitle(e.target.value)}
                autoFocus
                className="w-full bg-transparent text-[var(--fg)] focus:outline-none focus:bg-[var(--bg2)] rounded px-1 -mx-1 border border-transparent focus:border-[var(--bg3)]"
                style={headerTitleStyle}
                placeholder="Card title - one sentence, imperative ideally"
              />
            </>
          }
          actions={
            <>
              <button
                type="button"
                onClick={onClose}
                className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void form.handleJustCreate()}
                disabled={form.isSubmitting}
                className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs disabled:opacity-50"
                title={form.title.trim() ? 'Create without running' : 'Add a title first'}
              >
                Just create
              </button>
              <button
                type="button"
                onClick={() => void form.handleCreateAndRun()}
                disabled={form.isSubmitting}
                className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-colors text-sm font-medium inline-flex items-center gap-2 disabled:opacity-60"
                title={form.title.trim() ? 'Create and immediately run' : 'Add a title first'}
              >
                <span aria-hidden="true">▶</span>
                <span>{form.isSubmitting ? 'Creating…' : `Create & Run ${form.autonomous ? 'Auto' : 'HITL'}`}</span>
              </button>
            </>
          }
        />

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
        open={form.pendingTemplate !== null}
        title={form.pendingTemplate ? `Load template for "${form.pendingTemplate.type}"?` : ''}
        message="This will replace the current body."
        confirmLabel="Load template"
        onConfirm={() => {
          if (form.pendingTemplate) {
            form.setBody(form.pendingTemplate.body);
            form.setBodyDirty(false);
          }
          form.setPendingTemplate(null);
        }}
        onCancel={() => form.setPendingTemplate(null)}
      />
    </>
  );
}
