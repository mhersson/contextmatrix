import { memo, useMemo } from 'react';
import type { LogEntry } from '../../types';
import { TimestampLabel } from '../../utils/chatTimestamp';
import { ChatMarkdown } from './ChatMarkdown';
import { ClampedMarkdown } from './ClampedMarkdown';
import { CollapsiblePayload } from './CollapsiblePayload';
import { SpeakerChip } from './SpeakerChip';
import {
  TEXT_CLAMP_CHAR_THRESHOLD,
  TEXT_CLAMP_LINE_THRESHOLD,
  accentFor,
  countNewlines,
  parseStructured,
  textFor,
} from './chatEntryUtils';
import { StructuredPayload } from './StructuredPayload';
import { PromptActions } from './PromptActions';
import { parsePromptEntry, type PromptState } from './prompts';

interface ChatEntryProps {
  entry: LogEntry;
  /** Timestamp label parts; both undefined when the row shows no stamp.
   *  Primitives (not an object) so the memo's shallow compare holds. */
  stampHHMM?: string;
  stampTitle?: string;
  /** State of an architect prompt row; undefined on every other row. */
  promptStatus?: PromptState['status'];
  /** The answering action's label; null when a free-text reply answered. */
  promptAnswer?: string | null;
  /** Sends a reply; set only on the open prompt's row. */
  onReply?: (text: string) => Promise<void>;
}

function ChatEntryImpl({ entry, stampHHMM, stampTitle, promptStatus, promptAnswer, onReply }: ChatEntryProps) {
  const stamp = stampHHMM !== undefined && stampTitle !== undefined
    ? { hhmm: stampHHMM, title: stampTitle }
    : null;

  // Entries are reference-stable ring-buffer rows, so each body parses at
  // most once per mount. A structural row (divider, prompt) is never JSON.
  const structured = useMemo(
    () => (entry.type === 'text' && !entry.kind ? parseStructured(entry.content) : null),
    [entry],
  );
  const prompt = parsePromptEntry(entry);

  // Structural divider sentinel (kind="divider") rendered as a horizontal
  // rule with a small inline label rather than the normal system message
  // style. The match is on kind (not content) so the rendering survives
  // localised label changes and is unambiguous on REST-bootstrap reload.
  if (entry.kind === 'divider') {
    return (
      <div
        className="flex items-center gap-3 py-2"
        data-testid="chat-divider"
        role="separator"
        aria-label={entry.content || 'divider'}
      >
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
        <span
          className="text-[10px] uppercase tracking-wider font-mono"
          style={{ color: 'var(--grey1)' }}
        >
          {entry.content || 'divider'}
        </span>
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
      </div>
    );
  }

  if (entry.type === 'user') {
    return (
      <div className="flex justify-end">
        <div className="flex flex-col items-end max-w-[85%] min-w-0">
          {stamp && <TimestampLabel hhmm={stamp.hhmm} title={stamp.title} dateTime={entry.ts} />}
          <div
            className="rounded-lg px-3 py-2 text-sm whitespace-pre-wrap wrap-anywhere max-w-full"
            style={{
              backgroundColor: 'var(--bg-blue)',
              boxShadow: 'inset 0 0 0 1px var(--chat-user-ring)',
              color: 'var(--fg)',
            }}
          >
            {entry.content}
          </div>
        </div>
      </div>
    );
  }

  if (entry.type === 'text') {
    const body = prompt ? (prompt.prompt.text ?? prompt.text) : entry.content;
    return (
      <div className="flex justify-start">
        <div className="flex flex-col items-start max-w-[85%] min-w-0">
          {stamp && <TimestampLabel hhmm={stamp.hhmm} title={stamp.title} dateTime={entry.ts} />}
          {entry.agent && <SpeakerChip author={entry.agent} model={entry.model} />}
          {/* The bubble is a shrink-to-fit flex item, so its width follows its
              min-content size. `wrap-anywhere` (not `break-words`, which the
              spec excludes from min-content sizing) keeps a long identifier
              from dictating that width; `max-w-full` is the hard clamp. */}
          <div
            className="rounded-lg px-3 py-2 text-sm wrap-anywhere max-w-full"
            style={{
              backgroundColor:
                prompt?.prompt.kind === 'question' ? 'var(--prompt-question-bg)' : 'var(--chat-assistant-bg)',
              // The bar and the accent ring ride the same inset shadow as the plain ring.
              boxShadow:
                prompt?.prompt.kind === 'question'
                  ? 'inset 5px 0 0 0 var(--prompt-question-accent), inset 0 0 0 1px var(--prompt-question-ring)'
                  : 'inset 0 0 0 1px var(--chat-assistant-ring)',
              color: 'var(--fg)',
            }}
            data-prompt-id={prompt?.prompt.id}
            data-prompt-kind={prompt?.prompt.kind}
          >
            {structured ? (
              <StructuredPayload pretty={structured} />
            ) : body.length > TEXT_CLAMP_CHAR_THRESHOLD ||
              countNewlines(body, TEXT_CLAMP_LINE_THRESHOLD) > TEXT_CLAMP_LINE_THRESHOLD ? (
              <ClampedMarkdown source={body} />
            ) : (
              <ChatMarkdown source={body} />
            )}
            {prompt && promptStatus && (
              <PromptActions
                prompt={prompt.prompt}
                status={promptStatus}
                answer={promptAnswer}
                onReply={onReply}
              />
            )}
          </div>
        </div>
      </div>
    );
  }

  if (entry.type === 'system') {
    return (
      <div
        className="pl-3 border-l-2 text-sm leading-relaxed break-words"
        style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
      >
        <ChatMarkdown source={entry.content} />
      </div>
    );
  }

  // Gap markers are short one-liners; everything else in the fallthrough
  // (tool_call, tool_result, thinking, stderr) can be a 32 KiB payload and
  // collapses to a preview so it never enters layout at full size.
  if (entry.type === 'gap') {
    return (
      <div
        className="pl-3 border-l-2 text-sm font-mono leading-relaxed whitespace-pre-wrap break-words"
        style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
      >
        {entry.content}
      </div>
    );
  }

  return (
    <CollapsiblePayload
      content={entry.content}
      accent={accentFor(entry.type)}
      textColor={textFor(entry.type)}
    />
  );
}

/** Memoized row: ring-buffer entries are immutable and reference-stable, so a
 *  shallow compare skips re-rendering (and re-parsing markdown for) every
 *  already-mounted row when the transcript grows. */
export const ChatEntry = memo(ChatEntryImpl);
