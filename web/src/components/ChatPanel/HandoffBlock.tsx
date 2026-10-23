import { useState } from 'react';
import { Link } from 'react-router';
import { ChatMarkdown } from './ChatMarkdown';
import { formatBytes, utf8Length } from './chatEntryUtils';
import type { Handoff, HandoffState } from './handoff';

interface HandoffBlockProps {
  /** The parsed hand-off; null when the row could not be read. */
  handoff: Handoff | null;
  status: HandoffState['status'];
  blueprintId?: string;
  blueprintTitle?: string;
  /** Opens the review dialog; set only on an open hand-off in a chat that can start one. */
  onReview?: () => void;
}

export function HandoffBlock({ handoff, status, blueprintId, blueprintTitle, onReview }: HandoffBlockProps) {
  const [showNeed, setShowNeed] = useState(false);

  if (!handoff) {
    return (
      <div className="text-xs font-mono italic" style={{ color: 'var(--grey1)' }}>
        A hand-off that could not be read
      </div>
    );
  }

  return (
    <div
      className="rounded-lg px-3 py-2 text-sm max-w-[85%] min-w-0 wrap-anywhere"
      style={{
        backgroundColor: 'var(--chat-assistant-bg)',
        boxShadow: 'inset 0 0 0 1px var(--chat-assistant-ring)',
        color: 'var(--fg)',
      }}
      data-testid="handoff-block"
    >
      <div className="font-medium">{`Blueprint hand-off: ${handoff.title}`}</div>
      <div className="mt-1 text-xs" style={{ color: 'var(--grey2)' }}>
        {`Projects to change: ${handoff.projects.join(', ')}`}
      </div>
      <div className="text-xs" style={{ color: 'var(--grey2)' }}>
        {`Need: ${formatBytes(utf8Length(handoff.need))}`}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-3">
        {status === 'open' && onReview && (
          <button
            type="button"
            className="bf-btn-primary bf-btn-sm"
            style={{ background: 'var(--green)' }}
            onClick={onReview}
          >
            {'Review and start blueprint…'}
          </button>
        )}
        {status === 'replaced' && (
          <span className="text-xs" style={{ color: 'var(--grey1)' }}>
            Replaced by a newer hand-off
          </span>
        )}
        {status === 'started' && blueprintId && (
          <span className="text-xs" style={{ color: 'var(--grey2)' }}>
            {'Blueprint started: '}
            <Link to={`/chat/${blueprintId}`} className="underline" style={{ color: 'var(--green)' }}>
              {blueprintTitle}
            </Link>
          </span>
        )}
        <button
          type="button"
          className="text-xs underline"
          style={{ color: 'var(--grey2)' }}
          aria-expanded={showNeed}
          onClick={() => setShowNeed((v) => !v)}
        >
          {showNeed ? 'Hide need' : 'Show need'}
        </button>
      </div>
      {showNeed && (
        <div className="mt-2">
          <ChatMarkdown source={handoff.need} />
        </div>
      )}
    </div>
  );
}
