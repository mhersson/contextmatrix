import { useCallback, useEffect, useMemo, useRef, type ReactNode } from 'react';
import type { LogEntry } from '../../types';
import { EdgeStrip } from './EdgeStrip';
import { ScaleBar } from './ScaleBar';
import { Sheet } from './Sheet';
import { publishSheetButton, registerSheetToggle } from './sheetButtonStore';
import { BlueprintSheetContext } from './sheetContext';
import { gatePromptFor, resolveShown } from './sheetModel';
import { stageText } from './stages';
import type { BlueprintState } from './state';
import { usePaneWidth } from './usePaneWidth';
import { useSheetDivider } from './useSheetDivider';
import { useSheetState, type SheetLayout } from './useSheetState';

/** Pane width from which the sheet sits beside the transcript. */
export const SPLIT_MIN_WIDTH = 760;
/** Pane width under which the current-stage label moves into the scale bar's tooltip. */
export const LABEL_MIN_WIDTH = 560;

interface BlueprintPaneProps {
  chatId: string;
  state: BlueprintState;
  onRetry: () => void;
  logs: readonly LogEntry[];
  /** The session takes sends and its stream is connected, as for transcript prompts. */
  promptsLive: boolean;
  onReply: (text: string) => Promise<void>;
  /** Phone layout: a pane header button toggles a full-pane sheet. */
  phone: boolean;
  /** The transcript. */
  children: ReactNode;
}

export function BlueprintPane({ chatId, state, onRetry, logs, promptsLive, onReply, phone, children }: BlueprintPaneProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const width = usePaneWidth(rootRef);
  const layout: SheetLayout | null = phone
    ? 'phone'
    : width === null
      ? null
      : width >= SPLIT_MIN_WIDTH
        ? 'split'
        : 'overlay';
  const view = state.status === 'ready' ? state.view : null;
  const draft = state.status === 'ready' ? state.draft : null;
  const sheet = useSheetState(view, draft, layout ?? 'overlay');
  const phoneButton = layout === 'phone' && (state.status === 'ready' || state.status === 'unreachable');
  useEffect(() => {
    publishSheetButton(chatId, phoneButton ? { open: sheet.open, hasNew: sheet.hasNew } : null);
  }, [chatId, phoneButton, sheet.open, sheet.hasNew]);
  useEffect(() => () => publishSheetButton(chatId, null), [chatId]);
  useEffect(() => registerSheetToggle(chatId, sheet.toggle), [chatId, sheet.toggle]);
  const divider = useSheetDivider(bodyRef);
  const openGateId = view?.open?.kind === 'gate' ? view.open.prompt_id : undefined;
  const gatePrompt = useMemo(() => gatePromptFor(logs, promptsLive, openGateId), [logs, promptsLive, openGateId]);
  const { show, close } = sheet;
  const overlayRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLButtonElement>(null);
  const returnFocusRef = useRef(false);
  const closeOverlay = useCallback(() => {
    returnFocusRef.current = overlayRef.current?.contains(document.activeElement) ?? false;
    close();
  }, [close]);
  const actions = useMemo(() => ({ openArtifact: (stage: string, version?: number) => show(stage, version ?? null) }), [show]);

  const hasSheet = layout !== null && (state.status === 'ready' || state.status === 'unreachable');
  const overlayOpen = hasSheet && layout !== 'split' && sheet.open;
  useEffect(() => {
    if (overlayOpen) {
      overlayRef.current?.focus();
    } else if (returnFocusRef.current) {
      returnFocusRef.current = false;
      stripRef.current?.focus();
    }
  }, [overlayOpen]);
  const shown = view ? resolveShown(view, draft, sheet.pin) : null;
  const stripLabel = shown ? stageText(shown.stage).tab : 'Blueprint';
  const sheetEl = view ? (
    <Sheet
      chatId={chatId}
      view={view}
      draft={draft}
      shown={shown}
      onShow={show}
      gatePrompt={gatePrompt}
      onReply={onReply}
      onClose={closeOverlay}
    />
  ) : (
    <SheetUnavailable onRetry={onRetry} onClose={layout === 'split' ? undefined : closeOverlay} />
  );

  return (
    <BlueprintSheetContext.Provider value={view ? actions : null}>
      <div ref={rootRef} className="bp-pane">
        {view && (
          <ScaleBar view={view} compact={width !== null && width < LABEL_MIN_WIDTH} onOpenStage={(stage) => show(stage, null)} />
        )}
        <div ref={bodyRef} className="bp-body-row">
          <div className="bp-transcript">{children}</div>
          {hasSheet && layout === 'split' && sheet.open && (
            <>
              <div className="bp-divider" {...divider.separatorProps} />
              <div className="bp-sheet-slot" style={{ flexBasis: `${divider.percent}%` }}>
                {sheetEl}
              </div>
            </>
          )}
          {hasSheet && layout !== 'phone' && !sheet.open && (
            <EdgeStrip label={stripLabel} hasNew={sheet.hasNew} onOpen={sheet.openSheet} buttonRef={stripRef} />
          )}
          {overlayOpen && (
            <div
              ref={overlayRef}
              tabIndex={-1}
              className={`bp-overlay${layout === 'phone' ? ' bp-overlay--phone' : ''}`}
              onKeyDown={(e) => {
                if (e.key === 'Escape' && !e.defaultPrevented) closeOverlay();
              }}
            >
              {sheetEl}
            </div>
          )}
        </div>
      </div>
    </BlueprintSheetContext.Provider>
  );
}

function SheetUnavailable({ onRetry, onClose }: { onRetry: () => void; onClose?: () => void }) {
  return (
    <section className="bp-sheet bp-sheet--unavailable" aria-label="Blueprint sheet">
      <p>Can&apos;t reach the architect.</p>
      <button type="button" className="bf-btn-ghost bf-btn-sm" onClick={onRetry}>
        Retry
      </button>
      {onClose && (
        <button type="button" className="bf-btn-ghost bf-btn-sm" onClick={onClose}>
          Close
        </button>
      )}
    </section>
  );
}
