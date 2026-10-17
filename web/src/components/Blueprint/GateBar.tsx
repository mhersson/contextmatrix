import { PromptActions } from '../ChatPanel/PromptActions';
import type { ChatPrompt } from '../ChatPanel/prompts';
import { stageText } from './stages';

interface GateBarProps {
  stage: string;
  version: number;
  prompt: ChatPrompt;
  onReply: (text: string) => Promise<void>;
}

/** The open gate's actions repeated on the sheet; a click replies the way the transcript buttons do. */
export function GateBar({ stage, version, prompt, onReply }: GateBarProps) {
  return (
    <div className="bp-gatebar" role="region" aria-label="Gate decision">
      <span>
        <b>
          {stageText(stage).tab} rev {version}
        </b>{' '}
        is waiting for your decision
      </span>
      <PromptActions prompt={prompt} status="open" onReply={onReply} />
    </div>
  );
}
