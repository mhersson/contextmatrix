import { useId, useState } from 'react';
import { PromptActions } from '../ChatPanel/PromptActions';
import type { ChatPrompt } from '../ChatPanel/prompts';
import { BarrierIcon } from './icons';
import { FindingList } from './FindingList';
import { blockingFindings, findingStatus } from './ledger';
import type { GatePrompt, ShownReview } from './sheetModel';
import { stageText } from './stages';

interface GateBarProps {
  stage: string;
  version: number;
  prompt: ChatPrompt;
  status: GatePrompt['status'];
  /** The review of the version at the gate itself; null when it has none. */
  review: ShownReview | null;
  onReply: (text: string) => Promise<void>;
}

/** The open gate's actions repeated on the sheet; a click replies the way the transcript buttons do. */
export function GateBar({ stage, version, prompt, status, review, onReply }: GateBarProps) {
  return (
    <div className="bp-gatebar" role="region" aria-label="Gate decision">
      <BarrierIcon />
      <span>
        <b>
          {stageText(stage).tab} rev {version}
        </b>{' '}
        is waiting for your decision
      </span>
      <PromptActions prompt={prompt} status={status} onReply={onReply} />
      {review && review.review.findings.length > 0 && <GateLedger review={review} />}
    </div>
  );
}

function GateLedger({ review: { review } }: { review: ShownReview }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <div className="bp-gate-ledger">
      {blockingFindings(review).map((f, i) => (
        <p key={i} className="bp-gate-block">
          <b>{[f.id, 'critical'].filter(Boolean).join(' ')}</b>, {findingStatus(f)?.text ?? 'open'}: {f.text}
        </p>
      ))}
      <button
        type="button"
        className="bp-gate-toggle"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        Findings ({review.findings.length})
      </button>
      {open && (
        <div id={id}>
          <FindingList findings={review.findings} />
        </div>
      )}
    </div>
  );
}
