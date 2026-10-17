import type { CSSProperties } from 'react';
import type { BlueprintPipelineStep, BlueprintView } from '../../types';
import {
  costLabel,
  currentStep,
  describeCurrent,
  money,
  personaName,
  stageText,
  stepStatusText,
  visibleSteps,
  type Tone,
} from './stages';

interface ScaleBarProps {
  view: BlueprintView;
  /** Under 560px pane width: the current-stage label moves into the bar's tooltip. */
  compact: boolean;
  onOpenStage: (stage: string) => void;
}

type SegmentTone = 'done' | 'running' | 'waiting' | 'failed' | 'pending';

function segmentTone(step: BlueprintPipelineStep, isCurrent: boolean, tone: Tone | undefined): SegmentTone {
  if (isCurrent && (tone === 'running' || tone === 'waiting')) return tone;
  if (step.status === 'done' || step.status === 'folded') return 'done';
  if (step.status === 'failed') return 'failed';
  return 'pending';
}

export function ScaleBar({ view, compact, onOpenStage }: ScaleBarProps) {
  const current = currentStep(view);
  const text = describeCurrent(view);
  const cost = costLabel(view.cost);
  const steps = visibleSteps(view);
  const lineClass = text?.tone === 'waiting' ? 'bp-ask' : text?.tone === 'failed' ? 'bp-failed' : undefined;
  return (
    <div className="bp-scale">
      <div
        className="bp-scale-bar"
        role="list"
        aria-label="Blueprint stages"
        title={compact && text ? `${text.name}: ${text.line}` : undefined}
      >
        {steps.map((s) => (
          <Segment
            key={s.stage}
            step={s}
            view={view}
            tone={segmentTone(s, s === current, text?.tone)}
            onOpen={onOpenStage}
          />
        ))}
      </div>
      {!compact && text && (
        <div className="bp-scale-where" role="status">
          <b>{text.name}</b>
          <span className={lineClass}>{text.line}</span>
        </div>
      )}
      <div className={`bp-cost bp-cost--${cost.tone}`}>
        <span>{cost.text}</span>
        {cost.percent !== null && (
          <i aria-hidden="true" style={{ '--bp-cost-pct': `${cost.percent}%` } as CSSProperties} />
        )}
      </div>
    </div>
  );
}

interface SegmentProps {
  step: BlueprintPipelineStep;
  view: BlueprintView;
  tone: SegmentTone;
  onOpen: (stage: string) => void;
}

function Segment({ step, view, tone, onOpen }: SegmentProps) {
  const st = stageText(step.stage);
  const status = stepStatusText(step, view);
  const cost = step.cost_usd > 0 ? `, ${money(step.cost_usd)}` : '';
  const head = `${st.name}${step.gate ? ', gate' : ''}`;
  const detail = `${personaName(step.persona)}. ${status}${cost}`;
  const label = `${head}. ${detail}`;
  const body = (
    <>
      {step.gate && <span className="bp-seg-notch" aria-hidden="true" />}
      <span className="bp-seg-rule" aria-hidden="true" />
      <span className="bp-seg-tip" aria-hidden="true">
        {head}
        <small>{detail}</small>
      </span>
    </>
  );
  const openable = tone === 'done' && view.artifacts.some((a) => a.stage === step.stage);
  return (
    <div role="listitem" data-tone={tone} className={`bp-seg bp-seg--${tone}${step.status === 'folded' ? ' bp-seg--folded' : ''}`}>
      {openable ? (
        <button type="button" className="bp-seg-hit" aria-label={`Open ${st.tab}: ${label}`} onClick={() => onOpen(step.stage)}>
          {body}
        </button>
      ) : (
        <span className="bp-seg-hit" role="img" aria-label={label} tabIndex={0}>
          {body}
        </span>
      )}
    </div>
  );
}
