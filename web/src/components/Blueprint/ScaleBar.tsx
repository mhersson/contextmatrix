import type { BlueprintPipelineStep, BlueprintView } from '../../types';
import { CostCell } from './CostCell';
import {
  artifactsFor,
  barGroups,
  currentStep,
  describeCurrent,
  money,
  personaName,
  sliceOf,
  sliceSuffix,
  stageText,
  stepStatusText,
  type Tone,
} from './stages';

interface ScaleBarProps {
  chatId: string;
  view: BlueprintView;
  /** Under 560px pane width: the current-stage label moves into the bar's tooltip. */
  compact: boolean;
  onOpenStage: (stage: string, slice: string) => void;
}

type SegmentTone = 'done' | 'running' | 'waiting' | 'failed' | 'pending';

function segmentTone(step: BlueprintPipelineStep, isCurrent: boolean, tone: Tone | undefined): SegmentTone {
  if (isCurrent && (tone === 'running' || tone === 'waiting')) return tone;
  if (step.status === 'done' || step.status === 'folded') return 'done';
  if (step.status === 'failed') return 'failed';
  return 'pending';
}

export function ScaleBar({ chatId, view, compact, onOpenStage }: ScaleBarProps) {
  const current = currentStep(view);
  const text = describeCurrent(view);
  const groups = barGroups(view);
  const steps = groups.flatMap((g) => g.steps);
  const lineClass = text?.tone === 'waiting' ? 'bp-ask' : text?.tone === 'failed' ? 'bp-failed' : undefined;
  const segment = (s: BlueprintPipelineStep) => {
    const i = steps.indexOf(s);
    return (
      <Segment
        key={`${s.stage}/${sliceOf(s)}`}
        step={s}
        view={view}
        tone={segmentTone(s, s === current, text?.tone)}
        first={i === 0}
        end={i >= steps.length - 3}
        onOpen={onOpenStage}
      />
    );
  };
  return (
    <div className="bp-scale">
      <div
        className={`bp-scale-bar${groups.some((g) => g.slice !== '') ? ' bp-scale-bar--grouped' : ''}`}
        role="list"
        aria-label="Blueprint stages"
        title={compact && text ? `${text.name}: ${text.line}` : undefined}
      >
        {groups.map((g) => {
          if (g.slice === '') return segment(g.steps[0]);
          const holdsCurrent = current !== null && g.steps.includes(current);
          const tone = holdsCurrent ? segmentTone(current, true, text?.tone) : 'pending';
          const title = holdsCurrent ? view.slices.find((x) => x.id === g.slice)?.title : undefined;
          return (
            // Each segment's label names its slice; the bracket only draws the grouping.
            <div key={g.slice} role="presentation" className={`bp-seg-group bp-seg-group--${tone}`} style={{ flex: g.steps.length }}>
              {g.steps.map(segment)}
              <span className="bp-seg-group-label" aria-hidden="true">
                {title ? `${g.slice} · ${title}` : g.slice}
              </span>
            </div>
          );
        })}
      </div>
      {!compact && text && (
        <div className="bp-scale-where" role="status">
          <b>{text.name}</b>
          <span className={lineClass}>{text.line}</span>
        </div>
      )}
      <CostCell chatId={chatId} cost={view.cost} />
    </div>
  );
}

interface SegmentProps {
  step: BlueprintPipelineStep;
  view: BlueprintView;
  tone: SegmentTone;
  /** Draws the bar's left edge. */
  first: boolean;
  /** One of the last three: its tooltip opens leftwards. */
  end: boolean;
  onOpen: (stage: string, slice: string) => void;
}

function Segment({ step, view, tone, first, end, onOpen }: SegmentProps) {
  const st = stageText(step.stage);
  const status = stepStatusText(step, view);
  const cost = step.cost_usd > 0 ? `, ${money(step.cost_usd)}` : '';
  const head = `${st.name}${sliceSuffix(view.slices, sliceOf(step))}${step.gate ? ', gate' : ''}`;
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
  const openable = tone === 'done' && artifactsFor(view, step.stage, sliceOf(step)).length > 0;
  return (
    <div role="listitem" data-tone={tone} className={`bp-seg bp-seg--${tone}${step.status === 'folded' ? ' bp-seg--folded' : ''}${first ? ' bp-seg--first' : ''}${end ? ' bp-seg--end' : ''}`}>
      {openable ? (
        <button type="button" className="bp-seg-hit" aria-label={`Open ${st.tab}: ${label}`} onClick={() => onOpen(step.stage, sliceOf(step))}>
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
