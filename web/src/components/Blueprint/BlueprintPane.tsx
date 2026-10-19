import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { setChatLiveData } from '../../hooks/useChatLiveData';
import type { BlueprintArtifactRef, BlueprintSlice, LogEntry } from '../../types';
import { ErrorBoundary } from '../ErrorBoundary';
import { EdgeStrip } from './EdgeStrip';
import { ScaleBar } from './ScaleBar';
import { newestRound, roundLeft } from '../QuestionRound/rounds';
import { Sheet } from './Sheet';
import { publishSheetButton, registerSheetToggle } from './sheetButtonStore';
import { BlueprintArtifactsContext, BlueprintSheetContext } from './sheetContext';
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
const NO_ARTIFACTS: readonly BlueprintArtifactRef[] = [];

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
  const spent = view?.cost.spent_usd;
  const mode = view?.mode;
  useEffect(() => {
    if (spent !== undefined && mode !== undefined) setChatLiveData(chatId, { blueprintSpentUsd: spent, blueprintMode: mode });
  }, [chatId, spent, mode]);
  const divider = useSheetDivider(bodyRef);
  const openGateId = view?.open?.kind === 'gate' ? view.open.prompt_id : undefined;
  const gatePrompt = useMemo(() => gatePromptFor(logs, promptsLive, openGateId), [logs, promptsLive, openGateId]);
  const openRoundId = view?.open?.kind === 'round' ? view.open.prompt_id : undefined;
  const openRoundLeft = useMemo(() => {
    const r = openRoundId ? newestRound(logs, openRoundId) : null;
    return r ? roundLeft(r) : null;
  }, [logs, openRoundId]);
  const { show, close } = sheet;
  // The split slot or the overlay, whichever holds the sheet.
  const hostRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLButtonElement>(null);
  const returnFocusRef = useRef(false);
  const closeSheet = useCallback(() => {
    returnFocusRef.current = hostRef.current?.contains(document.activeElement) ?? false;
    close();
  }, [close]);
  // Keep the previous array while ids and titles match, so the context changes only when the slices do.
  const [slices, setSlices] = useState<BlueprintSlice[]>([]);
  const next = view?.slices ?? [];
  if (next.length !== slices.length || next.some((s, i) => s.id !== slices[i].id || s.title !== slices[i].title)) setSlices(next);
  const actions = useMemo(
    () => ({ openArtifact: (stage: string, slice: string, version?: number) => show(stage, slice, version ?? null), slices }),
    [show, slices],
  );

  const hasSheet = layout !== null && (state.status === 'ready' || state.status === 'unreachable');
  const overlayOpen = hasSheet && layout !== 'split' && sheet.open;
  useEffect(() => {
    if (overlayOpen) hostRef.current?.focus();
  }, [overlayOpen]);
  // A close from inside the sheet unmounts the focused control; the strip takes the focus.
  useEffect(() => {
    if (sheet.open || !returnFocusRef.current) return;
    returnFocusRef.current = false;
    stripRef.current?.focus();
  }, [sheet.open]);
  const shown = view ? resolveShown(view, draft, sheet.pin) : null;
  const stripLabel = shown ? stageText(shown.stage).tab : 'Blueprint';
  // Renderer bugs stop at the paper's own boundary; anything else in the sheet blanks the sheet, not the
  // transcript, and reopening the sheet draws it afresh.
  const sheetEl = (
    <ErrorBoundary fallback={<SheetNotice text="This sheet could not be drawn." onClose={closeSheet} />}>
      {view ? (
        <Sheet
          chatId={chatId}
          view={view}
          draft={draft}
          shown={shown}
          onShow={show}
          picked={sheet.picked}
          gatePrompt={gatePrompt}
          onReply={onReply}
          onClose={closeSheet}
          roundLeft={openRoundLeft}
        />
      ) : (
        <SheetNotice
          text="Can't reach the architect."
          onRetry={onRetry}
          onClose={layout === 'split' ? undefined : closeSheet}
        />
      )}
    </ErrorBoundary>
  );

  return (
    <BlueprintSheetContext.Provider value={view ? actions : null}>
      <BlueprintArtifactsContext.Provider value={view?.artifacts ?? NO_ARTIFACTS}>
        <div ref={rootRef} className="bp-pane">
          {view && (
            <ErrorBoundary fallback={null}>
              <ScaleBar chatId={chatId} view={view} compact={width !== null && width < LABEL_MIN_WIDTH} onOpenStage={(stage, slice) => show(stage, slice, null)} />
            </ErrorBoundary>
          )}
          <div ref={bodyRef} className="bp-body-row">
            <div className="bp-transcript">{children}</div>
            {hasSheet && layout === 'split' && sheet.open && (
              <>
                <div className="bp-divider" {...divider.separatorProps} />
                <div ref={hostRef} className="bp-sheet-slot" style={{ flexBasis: `${divider.percent}%` }}>
                  {sheetEl}
                </div>
              </>
            )}
            {hasSheet && layout !== 'phone' && !sheet.open && (
              <EdgeStrip label={stripLabel} hasNew={sheet.hasNew} onOpen={sheet.openSheet} buttonRef={stripRef} />
            )}
            {overlayOpen && (
              <div
                ref={hostRef}
                tabIndex={-1}
                className={`bp-overlay${layout === 'phone' ? ' bp-overlay--phone' : ''}`}
                onKeyDown={(e) => {
                  if (e.key === 'Escape' && !e.defaultPrevented) closeSheet();
                }}
              >
                {sheetEl}
              </div>
            )}
          </div>
        </div>
      </BlueprintArtifactsContext.Provider>
    </BlueprintSheetContext.Provider>
  );
}

function SheetNotice({ text, onRetry, onClose }: { text: string; onRetry?: () => void; onClose?: () => void }) {
  return (
    <section className="bp-sheet bp-sheet--unavailable" aria-label="Blueprint sheet">
      <p>{text}</p>
      {onRetry && (
        <button type="button" className="bf-btn-ghost bf-btn-sm" onClick={onRetry}>
          Retry
        </button>
      )}
      {onClose && (
        <button type="button" className="bf-btn-ghost bf-btn-sm" onClick={onClose}>
          Close
        </button>
      )}
    </section>
  );
}
