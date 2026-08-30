import { memo, useState } from 'react';
import { ChatMarkdown } from './ChatMarkdown';

interface ClampedMarkdownProps {
  source: string;
}

/**
 * Long markdown message body (e.g. the mob-moderator briefing with its fenced
 * diff) clamped to a fixed height behind a Read more toggle. The clamp is
 * visual (max-height + overflow hidden), not a string cut: truncating markdown
 * could split a fenced block and break rendering. The transcript's
 * ResizeObserver re-pins the scroller when expansion changes the row height.
 */
function ClampedMarkdownImpl({ source }: ClampedMarkdownProps) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div>
      <div
        data-testid="clamped-markdown"
        className="relative"
        style={expanded ? undefined : { maxHeight: '15rem', overflow: 'hidden' }}
      >
        <ChatMarkdown source={source} />
        {!expanded && (
          <div
            aria-hidden="true"
            className="absolute inset-x-0 bottom-0 h-8 pointer-events-none"
            style={{ background: 'linear-gradient(transparent, var(--bg2))' }}
          />
        )}
      </div>
      <button
        type="button"
        aria-expanded={expanded}
        className="block mt-1 text-xs font-mono underline decoration-dotted cursor-pointer"
        style={{ color: 'var(--grey1)' }}
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? 'Show less' : `Read more (${(source.length / 1024).toFixed(1)} KB)`}
      </button>
    </div>
  );
}

export const ClampedMarkdown = memo(ClampedMarkdownImpl);
