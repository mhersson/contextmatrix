import { useState, useEffect, useCallback, useRef } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import type { Card, LogEntry, ProjectConfig, PatchCardInput } from '../../types';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelMetadata } from './CardPanelMetadata';
import { CardPanelAgent } from './CardPanelAgent';
import { CardPanelActivity } from './CardPanelActivity';
import { CardChat } from './CardChat';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useBranches } from '../../hooks/useBranches';

// Approximate height in px of the panel content above the editor on mobile
// (header bar ~57px + title section ~60px + type/priority/state row ~60px +
// agent section ~50px + description label ~20px + spacing ~33px).
const MOBILE_ABOVE_EDITOR_PX = 280;

// Panel switches to full-width mode at this breakpoint (matches .card-panel CSS).
const MOBILE_BREAKPOINT = 1024;

const DEFAULT_EDITOR_HEIGHT = 250;

/** Shallow equality check for string arrays (used for label comparison). */
function arraysEqual(a: string[] | undefined, b: string[] | undefined): boolean {
  const aa = a ?? [];
  const bb = b ?? [];
  if (aa.length !== bb.length) return false;
  for (let i = 0; i < aa.length; i++) {
    if (aa[i] !== bb[i]) return false;
  }
  return true;
}

/** True when the panel occupies the full viewport width. */
function isMobileLayout(): boolean {
  return window.innerWidth <= MOBILE_BREAKPOINT;
}

/**
 * Computes the editor height for mobile using the VisualViewport API.
 * VisualViewport.height shrinks when the on-screen keyboard appears, giving us
 * the precise usable height above the keyboard without any extra calculation.
 */
function computeMobileEditorHeight(): number {
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  return Math.max(120, vvh - MOBILE_ABOVE_EDITOR_PX);
}

interface CardPanelProps {
  card: Card;
  config: ProjectConfig;
  cardLogs?: LogEntry[];
  onClose: () => void;
  onSave: (updates: PatchCardInput) => Promise<void>;
  onClaim: (agentId: string) => Promise<void>;
  onRelease: (agentId: string) => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
  onRunCard: (interactive: boolean) => Promise<void>;
  onStopCard: () => Promise<void>;
}

export function CardPanel({
  card,
  config,
  cardLogs = [],
  onClose,
  onSave,
  onClaim,
  onRelease,
  onSubtaskClick,
  currentAgentId,
  onPromptAgentId,
  onRunCard,
  onStopCard,
}: CardPanelProps) {
  const { theme } = useTheme();
  const panelRef = useRef<HTMLDivElement>(null);
  const editorContainerRef = useRef<HTMLDivElement>(null);
  const [editedCard, setEditedCard] = useState(card);
  const [isSaving, setIsSaving] = useState(false);
  // Initialize collapsed state based on whether we start in HITL-running mode.
  const initialIsHITLRunning = card.runner_status === 'running' && !(card.autonomous ?? false);
  const [descriptionCollapsed, setDescriptionCollapsed] = useState(initialIsHITLRunning);
  const [automationCollapsed, setAutomationCollapsed] = useState(initialIsHITLRunning);
  const [labelsCollapsed, setLabelsCollapsed] = useState(initialIsHITLRunning);
  const [editorHeight, setEditorHeight] = useState<number>(
    isMobileLayout() ? computeMobileEditorHeight() : DEFAULT_EDITOR_HEIGHT,
  );

  useFocusTrap(panelRef, true);

  // Sync prop → local edited state on card identity change (render-time pattern).
  const [prevCard, setPrevCard] = useState(card);
  if (card !== prevCard) {
    setPrevCard(card);
    setEditedCard(card);
  }

  // Auto-collapse Description, Automation, and Labels on entering HITL running
  // mode (runner_status === 'running' AND NOT autonomous); expand on leaving
  // (including promotion mid-run). Only fires on transitions of the boolean so
  // manual re-expands survive re-renders while still running.
  const isHITLRunning = card.runner_status === 'running' && !(card.autonomous ?? false);
  const [prevIsHITLRunning, setPrevIsHITLRunning] = useState(initialIsHITLRunning);
  if (isHITLRunning !== prevIsHITLRunning) {
    setPrevIsHITLRunning(isHITLRunning);
    setDescriptionCollapsed(isHITLRunning);
    setAutomationCollapsed(isHITLRunning);
    setLabelsCollapsed(isHITLRunning);
  }

  const {
    branches,
    loading: branchesLoading,
    error: branchesError,
  } = useBranches(card.project, !!config.remote_execution?.enabled);

  // Dynamically resize the editor when the visual viewport changes (e.g.
  // on-screen keyboard appearing/disappearing on mobile). On desktop the editor
  // keeps its default fixed height.
  useEffect(() => {
    function updateHeight() {
      if (isMobileLayout()) {
        setEditorHeight(computeMobileEditorHeight());
      } else {
        setEditorHeight(DEFAULT_EDITOR_HEIGHT);
      }
    }

    // VisualViewport fires 'resize' when the keyboard opens/closes on mobile.
    window.visualViewport?.addEventListener('resize', updateHeight);
    // Also listen to window resize for orientation changes and desktop resizes.
    window.addEventListener('resize', updateHeight);

    // Set initial height based on current state.
    updateHeight();

    return () => {
      window.visualViewport?.removeEventListener('resize', updateHeight);
      window.removeEventListener('resize', updateHeight);
    };
  }, []);

  // Auto-scroll the editor textarea so the cursor line stays visible when
  // typing past the bottom of the visible editor area.
  useEffect(() => {
    const container = editorContainerRef.current;
    if (!container) return;

    // The MDEditor renders a hidden textarea that receives keyboard input.
    // We wait a tick for the editor to finish mounting before querying it.
    let textarea: HTMLTextAreaElement | null = null;

    function findTextarea() {
      textarea = container?.querySelector<HTMLTextAreaElement>(
        '.w-md-editor-text-input',
      ) ?? null;
      return textarea;
    }

    function handleInput() {
      if (!textarea) findTextarea();
      if (!textarea) return;

      // Compute the cursor's approximate vertical position within the textarea
      // by measuring how many lines precede the cursor and multiplying by the
      // computed line height.
      const { selectionEnd, value } = textarea;
      const textBeforeCursor = value.slice(0, selectionEnd);
      const linesBefore = textBeforeCursor.split('\n').length;

      const computedStyle = window.getComputedStyle(textarea);
      const lineHeight = parseFloat(computedStyle.lineHeight) || 20;
      const paddingTop = parseFloat(computedStyle.paddingTop) || 0;

      const cursorY = paddingTop + (linesBefore - 1) * lineHeight;

      // Scroll so the cursor line is visible, keeping one extra line of context.
      const visibleBottom = textarea.scrollTop + textarea.clientHeight;
      if (cursorY + lineHeight > visibleBottom) {
        textarea.scrollTop = cursorY + lineHeight - textarea.clientHeight + lineHeight;
      } else if (cursorY < textarea.scrollTop) {
        textarea.scrollTop = Math.max(0, cursorY - lineHeight);
      }
    }

    // Delay query so MDEditor has time to render its textarea.
    const timer = setTimeout(() => {
      findTextarea();
      if (textarea) {
        textarea.addEventListener('input', handleInput);
      }
    }, 100);

    return () => {
      clearTimeout(timer);
      textarea?.removeEventListener('input', handleInput);
    };
  }, []);

  const isDirty =
    editedCard.title !== card.title ||
    editedCard.state !== card.state ||
    editedCard.priority !== card.priority ||
    editedCard.body !== card.body ||
    !arraysEqual(editedCard.labels, card.labels) ||
    (editedCard.autonomous ?? false) !== (card.autonomous ?? false) ||
    (editedCard.use_opus_orchestrator ?? false) !== (card.use_opus_orchestrator ?? false) ||
    (editedCard.feature_branch ?? false) !== (card.feature_branch ?? false) ||
    (editedCard.create_pr ?? false) !== (card.create_pr ?? false) ||
    (editedCard.vetted ?? false) !== (card.vetted ?? false) ||
    (editedCard.base_branch ?? '') !== (card.base_branch ?? '');

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (isDirty) {
          if (window.confirm('Discard unsaved changes?')) onClose();
        } else {
          onClose();
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isDirty, onClose]);

  const handleSave = useCallback(async () => {
    if (!isDirty || isSaving) return;
    setIsSaving(true);
    try {
      const updates: PatchCardInput = {};
      if (editedCard.title !== card.title) updates.title = editedCard.title;
      if (editedCard.state !== card.state) updates.state = editedCard.state;
      if (editedCard.priority !== card.priority) updates.priority = editedCard.priority;
      if (editedCard.body !== card.body) updates.body = editedCard.body;
      if (JSON.stringify(editedCard.labels) !== JSON.stringify(card.labels)) {
        updates.labels = editedCard.labels;
      }
      if ((editedCard.autonomous ?? false) !== (card.autonomous ?? false)) {
        updates.autonomous = editedCard.autonomous ?? false;
      }
      if ((editedCard.use_opus_orchestrator ?? false) !== (card.use_opus_orchestrator ?? false)) {
        updates.use_opus_orchestrator = editedCard.use_opus_orchestrator ?? false;
      }
      if ((editedCard.feature_branch ?? false) !== (card.feature_branch ?? false)) {
        updates.feature_branch = editedCard.feature_branch ?? false;
      }
      if ((editedCard.create_pr ?? false) !== (card.create_pr ?? false)) {
        updates.create_pr = editedCard.create_pr ?? false;
      }
      if ((editedCard.vetted ?? false) !== (card.vetted ?? false)) {
        updates.vetted = editedCard.vetted ?? false;
      }
      if ((editedCard.base_branch ?? '') !== (card.base_branch ?? '')) {
        updates.base_branch = editedCard.base_branch ?? '';
      }
      await onSave(updates);
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, editedCard, card, onSave]);

  const handleClaim = useCallback(async () => {
    const agentId = currentAgentId || onPromptAgentId();
    if (agentId) await onClaim(agentId);
  }, [currentAgentId, onPromptAgentId, onClaim]);

  const handleRelease = useCallback(async () => {
    if (currentAgentId) await onRelease(currentAgentId);
  }, [currentAgentId, onRelease]);

  const handleClose = () => {
    if (isDirty) {
      if (window.confirm('Discard unsaved changes?')) onClose();
    } else {
      onClose();
    }
  };

  const canRun =
    config.remote_execution?.enabled !== false &&
    card.state === 'todo' &&
    (!card.runner_status || card.runner_status === 'failed' || card.runner_status === 'killed');

  // Split layout only for HITL sessions (running AND not autonomous).
  // `isHITLRunning` is computed once at the top of the component.

  const body = (
    <>
      <CardPanelAgent
        card={card}
        canClaim={!card.assigned_agent}
        canRelease={!!card.assigned_agent && card.assigned_agent === currentAgentId}
        onClaim={handleClaim}
        onRelease={handleRelease}
        canStop={card.runner_status === 'queued' || card.runner_status === 'running'}
        onStop={onStopCard}
      />

      <div ref={editorContainerRef} data-color-mode={theme}>
        <div className="flex items-center gap-1 mb-1">
          <label className="text-xs text-[var(--grey1)]">Description</label>
          <button
            onClick={() => setDescriptionCollapsed((v) => !v)}
            className="flex items-center justify-center text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
            aria-label={descriptionCollapsed ? 'Expand description' : 'Collapse description'}
          >
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d={descriptionCollapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'} />
            </svg>
          </button>
        </div>
        {!descriptionCollapsed && (
          <MDEditor
            value={editedCard.body}
            onChange={(val) => setEditedCard((prev) => ({ ...prev, body: val || '' }))}
            preview="edit"
            height={editorHeight}
            visibleDragbar={false}
            previewOptions={{ skipHtml: true }}
          />
        )}
      </div>

      <CardPanelMetadata
        card={card}
        editedLabels={editedCard.labels}
        onLabelsChange={(labels) => setEditedCard((prev) => ({ ...prev, labels }))}
        onSubtaskClick={onSubtaskClick}
        editedAutonomous={editedCard.autonomous ?? false}
        editedUseOpusOrchestrator={editedCard.use_opus_orchestrator ?? false}
        editedFeatureBranch={editedCard.feature_branch ?? false}
        editedCreatePR={editedCard.create_pr ?? false}
        onAutonomousChange={(v) => setEditedCard((prev) => ({ ...prev, autonomous: v, ...(v ? {} : { base_branch: undefined }) }))}
        onUseOpusOrchestratorChange={(v) => setEditedCard((prev) => ({ ...prev, use_opus_orchestrator: v }))}
        onFeatureBranchChange={(v) => setEditedCard((prev) => ({
          ...prev,
          feature_branch: v,
          create_pr: v ? prev.create_pr : false,
        }))}
        onCreatePRChange={(v) => setEditedCard((prev) => ({ ...prev, create_pr: v }))}
        editedVetted={editedCard.vetted ?? false}
        onVettedChange={(v) => setEditedCard((prev) => ({ ...prev, vetted: v }))}
        baseBranch={editedCard.base_branch}
        onBaseBranchChange={(v) => setEditedCard((prev) => ({ ...prev, base_branch: v || undefined }))}
        branches={branches}
        branchesLoading={branchesLoading}
        branchesError={branchesError}
        canRun={canRun}
        onRun={async () => {
          if (isDirty) {
            await handleSave();
          }
          await onRunCard(!(editedCard.autonomous ?? false));
        }}
        automationCollapsed={automationCollapsed}
        onToggleAutomation={() => setAutomationCollapsed((v) => !v)}
        labelsCollapsed={labelsCollapsed}
        onToggleLabels={() => setLabelsCollapsed((v) => !v)}
      />

      <CardPanelActivity activityLog={card.activity_log} />
    </>
  );

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={handleClose} />

      <div ref={panelRef} className="card-panel animate-panel-slide-in" role="dialog" aria-modal="true" aria-label="Card details">
        <CardPanelHeader
          card={card}
          editedCard={editedCard}
          config={config}
          isDirty={isDirty}
          isSaving={isSaving}
          onClose={onClose}
          onSave={handleSave}
          onTitleChange={(title) => setEditedCard((prev) => ({ ...prev, title }))}
          onPriorityChange={(priority) => setEditedCard((prev) => ({ ...prev, priority }))}
          onStateChange={(state) => setEditedCard((prev) => ({ ...prev, state }))}
        />

        {isHITLRunning ? (
          <div className="flex flex-col flex-1 min-h-0" data-testid="body-split">
            {/* Top scroll region — capped so chat always gets at least half the panel */}
            <div className="overflow-y-auto overflow-x-hidden p-4 space-y-4 max-h-[50%] min-h-0" data-testid="body-top-section">
              {body}
            </div>

            {/* Bottom chat region — fills remaining height */}
            <div className="flex-1 min-h-0 flex flex-col p-4 pt-0" data-testid="body-chat-region">
              <CardChat card={card} cardLogs={cardLogs} />
            </div>
          </div>
        ) : (
          <div className="p-4 space-y-4 overflow-y-auto overflow-x-hidden flex-1 min-h-0" data-testid="body-single">
            {body}
          </div>
        )}
      </div>
    </>
  );
}
