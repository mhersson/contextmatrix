import { useMemo } from 'react';
import { formatTokens } from '../../lib/format';
import type { BlueprintCurrentRun } from '../../types';
import { Block } from './BodyMarkdown';
import type { HeldDraft } from './draft';
import { splitBlocks } from './revisions';
import { personaName, stageText } from './stages';

/** The body as it streams: finished blocks render as markdown; the block being written shows as text with a caret. */
export function DraftBody({ draft }: { draft: HeldDraft }) {
  const blocks = useMemo(() => splitBlocks(draft.text), [draft.text]);
  const last = blocks[blocks.length - 1] ?? '';
  return (
    <div className="bp-draft">
      {draft.refused && <RefusedBand reason={draft.reason} />}
      {blocks.slice(0, -1).map((text, i) => (
        <Block key={i} text={text} />
      ))}
      {draft.refused ? (
        <Block text={last} />
      ) : (
        <p className="bp-writing">
          {last}
          <span className="bp-caret" aria-hidden="true" />
        </p>
      )}
    </div>
  );
}

export function RefusedBand({ reason }: { reason: string }) {
  return (
    <p className="bp-refused" role="status">
      Refused by checks{reason ? `: ${reason}` : ''}
    </p>
  );
}

export function ProgressLine({ run, stage }: { run: BlueprintCurrentRun; stage: string }) {
  return (
    <p className="bp-progress" role="status">
      <span className="bp-progress-rule" aria-hidden="true" />
      {personaName(run.persona)} is working on the {stageText(stage).noun}: turn {run.turns}, {run.tool_calls}{' '}
      {run.tool_calls === 1 ? 'tool call' : 'tool calls'}, {formatTokens(run.tokens)} tokens
    </p>
  );
}
