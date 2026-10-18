import { useRef, useState } from 'react';
import type { BlueprintView } from '../../types';
import { ErrorBoundary } from '../ErrorBoundary';
import { GateBar } from './GateBar';
import type { RoundLeft } from '../QuestionRound/rounds';
import { Paper } from './Paper';
import { SheetTabs } from './SheetTabs';
import { TitleBlock } from './TitleBlock';
import type { HeldDraft } from './draft';
import {
  reviewFor,
  reviewingPersona,
  sheetTabs,
  shownStatus,
  sliceForTab,
  sliceTabs,
  versionOptions,
  writingVersion,
  type GatePrompt,
  type Shown,
} from './sheetModel';
import { artifactsFor, showSlices, stepFor } from './stages';
import { useFollowEnd } from './useFollowEnd';

interface SheetProps {
  chatId: string;
  view: BlueprintView;
  draft: HeldDraft | null;
  shown: Shown | null;
  /** Pins a stage in a slice ("" for the whole blueprint), and a version when given. */
  onShow: (stage: string, slice: string, version: number | null) => void;
  /** The open gate's transcript prompt, matched on the view's open prompt id. */
  gatePrompt: GatePrompt | null;
  onReply: (text: string) => Promise<void>;
  onClose?: () => void;
  /** What the open round still waits on, when the stage waits on one. */
  roundLeft?: RoundLeft | null;
  /** The last slice picked; a stage tab returns to it. */
  picked: string;
}

export function Sheet({ chatId, view, draft, shown, onShow, gatePrompt, onReply, onClose, roundLeft, picked }: SheetProps) {
  const shownKey = shown ? `${shown.stage}/${shown.slice}/${shown.version}` : '';
  const [compare, setCompare] = useState(false);
  const [compareFor, setCompareFor] = useState(shownKey);
  if (compareFor !== shownKey) {
    setCompareFor(shownKey);
    setCompare(false);
  }
  const scrollRef = useRef<HTMLDivElement>(null);
  const writing = shown ? writingVersion(view, draft, shown.stage, shown.slice) : null;
  const isWriting = shown !== null && writing === shown.version;
  const shownDraft =
    shown &&
    isWriting &&
    draft &&
    draft.stage === shown.stage &&
    draft.slice === shown.slice &&
    draft.version === shown.version
      ? draft
      : null;
  useFollowEnd(scrollRef, shownDraft?.text);

  const refs = shown ? artifactsFor(view, shown.stage, shown.slice) : [];
  const stored = shown ? (refs.find((r) => r.version === shown.version) ?? null) : null;
  const canCompare = stored !== null && refs.some((r) => r.version < stored.version);
  const review = shown ? reviewFor(view, shown.stage, shown.slice, shown.version) : null;
  // The gate takes only the version's own review: an earlier one judged a version this gate is not deciding.
  const gateReview = shown && review?.version === shown.version ? review : null;

  return (
    <section className="bp-sheet" aria-label="Blueprint sheet">
      <SheetTabs
        tabs={sheetTabs(view, draft)}
        slices={sliceTabs(view, draft, shown)}
        stage={shown?.stage ?? null}
        grouped={showSlices(view.slices)}
        onStage={(stage) => onShow(stage, sliceForTab(view, draft, stage, [shown?.slice ?? '', picked]), null)}
        onSlice={(slice) => shown && onShow(shown.stage, slice, null)}
        onClose={onClose}
      />
      <div className="bp-paperwrap" ref={scrollRef}>
        {shown ? (
          // Keyed on the shown sheet, so another sheet draws afresh; the tabs stay outside and keep their focus.
          <ErrorBoundary
            key={shownKey}
            fallback={
              <p className="bp-load-error" role="alert">
                This sheet could not be drawn.
              </p>
            }
          >
            <Paper
              chatId={chatId}
              view={view}
              stage={shown.stage}
              slice={shown.slice}
              version={shown.version}
              writing={isWriting ? (view.current ?? null) : null}
              draft={shownDraft}
              compare={compare && canCompare}
              roundLeft={roundLeft}
            />
          </ErrorBoundary>
        ) : (
          <p className="bp-empty">Nothing on the sheet yet.</p>
        )}
      </div>
      {shown && gatePrompt && stored?.status === 'at_gate' && (
        <GateBar
          key={`gate/${shownKey}`}
          stage={shown.stage}
          version={shown.version}
          prompt={gatePrompt.prompt}
          status={gatePrompt.status}
          review={gateReview}
          onReply={onReply}
        />
      )}
      {shown && (
        <TitleBlock
          key={shownKey}
          stage={shown.stage}
          persona={stepFor(view, shown.stage, shown.slice)?.persona ?? ''}
          options={versionOptions(view, shown.stage, shown.slice, writing)}
          version={shown.version}
          onPickVersion={(v) => onShow(shown.stage, shown.slice, v)}
          review={review}
          reviewing={reviewingPersona(view, shown.stage, shown.slice, shown.version)}
          status={shownStatus(view, shown.stage, shown.slice, stored, isWriting, roundLeft ?? null)}
          compare={{ available: canCompare, on: compare, onToggle: () => setCompare((c) => !c) }}
        />
      )}
    </section>
  );
}
