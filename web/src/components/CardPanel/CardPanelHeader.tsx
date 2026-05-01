import { useId, useState } from 'react';
import type { Card, ProjectConfig } from '../../types';
import { headerTitleStyle } from '../../lib/header-tokens';
import {
  isRunnerAttached,
  primaryAction,
} from './utils';
import { CardPanelHeaderChips } from './CardPanelHeaderChips';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface CardPanelHeaderProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  currentAgentId: string | null;
  isDirty: boolean;
  isSaving: boolean;
  isDeleting: boolean;
  canRun: boolean;
  onClose: () => void;
  onSave: () => void;
  onTitleChange: (title: string) => void;
  onPriorityChange: (priority: string) => void;
  onTypeChange: (type: string) => void;
  onPrimaryAction: () => void;
  onStopCard: () => Promise<void>;
  onOpenDependency?: (depId: string) => void;
  firstUnfinishedDep?: string | null;
}

export function CardPanelHeader({
  card,
  editedCard,
  config,
  currentAgentId,
  isDirty,
  isSaving,
  isDeleting,
  canRun,
  onClose,
  onSave,
  onTitleChange,
  onPriorityChange,
  onTypeChange,
  onPrimaryAction,
  onStopCard,
  onOpenDependency,
  firstUnfinishedDep,
}: CardPanelHeaderProps) {
  const titleId = useId();
  const priorityId = useId();
  const typeId = useId();
  const [confirmStopOpen, setConfirmStopOpen] = useState(false);

  const runnerAttached = isRunnerAttached(card, currentAgentId);
  const primary = primaryAction(card, editedCard.autonomous ?? false, config, canRun);

  const showSave = !runnerAttached;
  const showBlockedHelper = card.state === 'blocked' && !!firstUnfinishedDep && !!onOpenDependency;

  const handleStopConfirm = async () => {
    setConfirmStopOpen(false);
    await onStopCard();
  };

  const renderPrimary = () => {
    if (!primary) return null;
    // primary.kind === 'stop' is unreachable here: the caller only invokes
    // renderPrimary from the non-runnerAttached branch, and isRunnerAttached
    // returns true exactly when primaryAction would return { kind: 'stop' }.
    // The stop button lives inline next to the "Agent still owns this card"
    // notice in the runnerAttached branch below.
    if (primary.kind === 'stop') return null;
    if (primary.kind === 'run') {
      return (
        <button
          type="button"
          onClick={onPrimaryAction}
          className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-opacity text-sm font-medium inline-flex items-center gap-2"
        >
          <span aria-hidden="true">▶</span>
          <span>{primary.autonomous ? 'Run Auto' : 'Run HITL'}</span>
        </button>
      );
    }
    return (
      <button
        type="button"
        onClick={onPrimaryAction}
        className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-opacity text-sm font-medium"
      >
        {primary.label}
      </button>
    );
  };

  return (
    <>
    <div className="flex flex-wrap items-start gap-x-4 gap-y-3 px-5 py-4 border-b border-[var(--bg3)]">
      {/* Title row — flex: 1 1 340px + min-width: 0 so it shrinks before wrapping the cluster. */}
      <div className="flex-1 min-w-0 flex flex-col gap-2" style={{ flexBasis: '340px' }}>
        <CardPanelHeaderChips
          card={card}
          editedCard={editedCard}
          config={config}
          runnerAttached={runnerAttached}
          onClose={onClose}
          onPriorityChange={onPriorityChange}
          priorityId={priorityId}
          onTypeChange={onTypeChange}
          typeId={typeId}
        />

        {card.source && !card.vetted && (
          <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded bg-[var(--bg-yellow)] text-[var(--yellow)] text-xs w-fit">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <span>Unvetted external content — agents cannot claim this card</span>
          </div>
        )}

        {/* Title: editable when detached; plain heading when owned. */}
        {runnerAttached ? (
          <h2 className="truncate text-[var(--fg)]" style={headerTitleStyle} title={editedCard.title}>
            {editedCard.title}
          </h2>
        ) : (
          <>
            <label htmlFor={titleId} className="sr-only">Title</label>
            <input
              id={titleId}
              type="text"
              value={editedCard.title}
              onChange={(e) => onTitleChange(e.target.value)}
              className="w-full bg-transparent text-[var(--fg)] focus:outline-none focus:bg-[var(--bg2)] rounded px-1 -mx-1 border border-transparent focus:border-[var(--bg3)]"
              style={headerTitleStyle}
              placeholder="Title"
            />
          </>
        )}
      </div>

      {/* Action cluster — wraps to the next line (still right-aligned via ml-auto) when crowded. */}
      <div className="flex items-center gap-2 ml-auto shrink-0 flex-wrap justify-end">
        {runnerAttached ? (
          <>
            <div
              role="status"
              className="inline-flex items-center gap-2 px-3 py-1.5 rounded text-xs bg-[var(--bg-yellow)] text-[var(--yellow)]"
            >
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2} aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01M5.07 19h13.86c1.54 0 2.5-1.67 1.73-3L13.73 4c-.77-1.33-2.69-1.33-3.46 0L3.34 16c-.77 1.33.19 3 1.73 3z" />
              </svg>
              <span>Agent still owns this card — transitions locked</span>
            </div>
            {(card.runner_status === 'queued' || card.runner_status === 'running') && (
              <button
                type="button"
                onClick={() => setConfirmStopOpen(true)}
                className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm font-medium inline-flex items-center gap-2"
                aria-label="Stop runner"
              >
                <span aria-hidden="true">■</span>
                <span>Stop</span>
              </button>
            )}
          </>
        ) : (
          <>
            {showSave && (
              <button
                type="button"
                onClick={onSave}
                disabled={!isDirty || isSaving || isDeleting}
                title="Save (⌘S)"
                className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                  isDirty && !isSaving
                    ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
                    : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
                }`}
              >
                {isSaving ? 'Saving…' : 'Save'}
              </button>
            )}
            {showBlockedHelper && firstUnfinishedDep && (
              <button
                type="button"
                onClick={() => onOpenDependency?.(firstUnfinishedDep)}
                className="px-3 py-1.5 rounded bg-[var(--bg2)] text-[var(--aqua)] hover:bg-[var(--bg3)] transition-colors text-sm"
              >
                Open dependency
              </button>
            )}
            {renderPrimary()}
          </>
        )}
      </div>
    </div>

    <ConfirmModal
      open={confirmStopOpen}
      title="Stop this task?"
      message="The container will be destroyed and uncommitted work discarded."
      confirmLabel="Stop"
      variant="danger"
      onConfirm={() => void handleStopConfirm()}
      onCancel={() => setConfirmStopOpen(false)}
    />
    </>
  );
}
