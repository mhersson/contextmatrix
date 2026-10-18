import { useRef, useState } from 'react';
import type { BlueprintView } from '../../types';
import { ErrorBoundary } from '../ErrorBoundary';
import { GateBar } from './GateBar';
import type { RoundLeft } from '../QuestionRound/rounds';
import { Paper } from './Paper';
import { TitleBlock } from './TitleBlock';
import type { HeldDraft } from './draft';
import {
  reviewFor,
  reviewingPersona,
  sheetTabs,
  shownStatus,
  versionOptions,
  writingVersion,
  type GatePrompt,
  type Shown,
} from './sheetModel';
import { artifactsFor } from './stages';
import { useFollowEnd } from './useFollowEnd';

interface SheetProps {
  chatId: string;
  view: BlueprintView;
  draft: HeldDraft | null;
  shown: Shown | null;
  /** Pins a stage, and a version when given. */
  onShow: (stage: string, version: number | null) => void;
  /** The open gate's transcript prompt, matched on the view's open prompt id. */
  gatePrompt: GatePrompt | null;
  onReply: (text: string) => Promise<void>;
  onClose?: () => void;
  /** What the open round still waits on, when the stage waits on one. */
  roundLeft?: RoundLeft | null;
}

export function Sheet({ chatId, view, draft, shown, onShow, gatePrompt, onReply, onClose, roundLeft }: SheetProps) {
  const tabs = sheetTabs(view, draft);
  const shownKey = shown ? `${shown.stage}/${shown.version}` : '';
  const [compare, setCompare] = useState(false);
  const [compareFor, setCompareFor] = useState(shownKey);
  if (compareFor !== shownKey) {
    setCompareFor(shownKey);
    setCompare(false);
  }
  const scrollRef = useRef<HTMLDivElement>(null);
  const writing = shown ? writingVersion(view, draft, shown.stage) : null;
  const isWriting = shown !== null && writing === shown.version;
  const shownDraft =
    shown && isWriting && draft && draft.stage === shown.stage && draft.version === shown.version ? draft : null;
  useFollowEnd(scrollRef, shownDraft?.text);

  const refs = shown ? artifactsFor(view, shown.stage) : [];
  const stored = shown ? (refs.find((r) => r.version === shown.version) ?? null) : null;
  const canCompare = stored !== null && refs.some((r) => r.version < stored.version);
  const review = shown ? reviewFor(view, shown.stage, shown.version) : null;
  // The gate takes only the version's own review: an earlier one judged a version this gate is not deciding.
  const gateReview = shown && review?.version === shown.version ? review : null;

  return (
    <section className="bp-sheet" aria-label="Blueprint sheet">
      <div className="bp-tabs" role="tablist" aria-label="Artifacts">
        {tabs.map((t) => (
          <button
            key={t.stage}
            type="button"
            role="tab"
            aria-selected={shown?.stage === t.stage}
            disabled={!t.enabled}
            className={`bp-tab${shown?.stage === t.stage ? ' bp-tab--on' : ''}`}
            onClick={() => onShow(t.stage, null)}
          >
            {t.label}
            {t.current && (
              <span className="bp-tab-dot">
                <span className="sr-only">, current stage</span>
              </span>
            )}
          </button>
        ))}
        <span className="bp-tabs-fill" />
        {onClose && (
          <button type="button" className="bp-tabs-close" onClick={onClose}>
            Close
          </button>
        )}
      </div>
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
          persona={view.pipeline.find((s) => s.stage === shown.stage)?.persona ?? ''}
          options={versionOptions(view, shown.stage, writing)}
          version={shown.version}
          onPickVersion={(v) => onShow(shown.stage, v)}
          review={review}
          reviewing={reviewingPersona(view, shown.stage, shown.version)}
          status={shownStatus(view, shown.stage, stored, isWriting, roundLeft ?? null)}
          compare={{ available: canCompare, on: compare, onToggle: () => setCompare((c) => !c) }}
        />
      )}
    </section>
  );
}
