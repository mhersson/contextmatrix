import { useState, useEffect, useCallback, useRef } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import type { Card, ProjectConfig, PatchCardInput } from '../../types';
import { api } from '../../api/client';
import { CardPanelHeader } from './CardPanelHeader';
import { CardPanelMetadata } from './CardPanelMetadata';
import { CardPanelAgent } from './CardPanelAgent';
import { CardPanelActivity } from './CardPanelActivity';
import { useFocusTrap } from '../../hooks/useFocusTrap';

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
  onClose: () => void;
  onSave: (updates: PatchCardInput) => Promise<void>;
  onClaim: (agentId: string) => Promise<void>;
  onRelease: (agentId: string) => Promise<void>;
  onSubtaskClick: (cardId: string) => void;
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
  onRunCard: () => Promise<void>;
  onStopCard: () => Promise<void>;
}

export function CardPanel({
  card,
  config,
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
  const [branches, setBranches] = useState<string[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(false);
  const [branchesError, setBranchesError] = useState(false);
  const [editorHeight, setEditorHeight] = useState<number>(
    isMobileLayout() ? computeMobileEditorHeight() : DEFAULT_EDITOR_HEIGHT,
  );

  useFocusTrap(panelRef, true);

  useEffect(() => {
    setEditedCard(card);
  }, [card]);

  useEffect(() => {
    if (!config.remote_execution?.enabled) return;
    let cancelled = false;
    setBranchesLoading(true);
    setBranchesError(false);
    api.fetchBranches(card.project).then((data) => {
      if (!cancelled) setBranches(data);
    }).catch(() => {
      if (!cancelled) setBranchesError(true);
    }).finally(() => {
      if (!cancelled) setBranchesLoading(false);
    });
    return () => { cancelled = true; };
  }, [card.project, config.remote_execution?.enabled]);

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

        <div className="p-4 space-y-4 overflow-y-auto overflow-x-hidden flex-1 min-h-0">
          <CardPanelAgent
            card={card}
            canClaim={!card.assigned_agent}
            canRelease={!!card.assigned_agent && card.assigned_agent === currentAgentId}
            onClaim={handleClaim}
            onRelease={handleRelease}
            canRun={!!card.autonomous && card.state === 'todo' && (!card.runner_status || card.runner_status === 'failed' || card.runner_status === 'killed') && config.remote_execution?.enabled !== false}
            canStop={card.runner_status === 'queued' || card.runner_status === 'running'}
            onRun={onRunCard}
            onStop={onStopCard}
          />

          <div ref={editorContainerRef} data-color-mode={theme}>
            <label className="block text-xs text-[var(--grey1)] mb-1">Description</label>
            <MDEditor
              value={editedCard.body}
              onChange={(val) => setEditedCard((prev) => ({ ...prev, body: val || '' }))}
              preview="edit"
              height={editorHeight}
              visibleDragbar={false}
            />
          </div>

          <CardPanelMetadata
            card={card}
            editedLabels={editedCard.labels}
            onLabelsChange={(labels) => setEditedCard((prev) => ({ ...prev, labels }))}
            onSubtaskClick={onSubtaskClick}
            editedAutonomous={editedCard.autonomous ?? false}
            editedFeatureBranch={editedCard.feature_branch ?? false}
            editedCreatePR={editedCard.create_pr ?? false}
            onAutonomousChange={(v) => setEditedCard((prev) => ({ ...prev, autonomous: v, ...(v ? {} : { base_branch: undefined }) }))}
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
          />

          <CardPanelActivity activityLog={card.activity_log} />
        </div>
      </div>
    </>
  );
}
