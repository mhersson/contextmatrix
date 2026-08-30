import { memo, useMemo, useState } from 'react';
import {
  COLLAPSE_CHAR_THRESHOLD,
  COLLAPSE_LINE_THRESHOLD,
  PREVIEW_CHAR_LIMIT,
  PREVIEW_LINE_LIMIT,
  countNewlines,
} from './chatEntryUtils';

interface CollapsiblePayloadProps {
  content: string;
  accent: string;
  textColor: string;
}

/** Cut the string preview at PREVIEW_LINE_LIMIT lines within PREVIEW_CHAR_LIMIT
 *  characters. The STRING is truncated (not CSS-clamped) so a 32 KiB payload
 *  never enters layout while collapsed. */
function buildPreview(content: string): string {
  const preview = content.slice(0, PREVIEW_CHAR_LIMIT);
  let idx = -1;
  for (let n = 0; n < PREVIEW_LINE_LIMIT; n++) {
    idx = preview.indexOf('\n', idx + 1);
    if (idx === -1) return preview;
  }
  return preview.slice(0, idx);
}

/**
 * Plain-text log payload (tool calls, tool results, thinking, stderr) that
 * collapses large content to a short preview with an explicit expand toggle.
 * Expansion state is local and ephemeral; row keys are seq-stable so it
 * survives transcript appends.
 */
function CollapsiblePayloadImpl({ content, accent, textColor }: CollapsiblePayloadProps) {
  const [expanded, setExpanded] = useState(false);

  const { collapsible, preview } = useMemo(() => {
    const needsCollapse =
      content.length > COLLAPSE_CHAR_THRESHOLD ||
      countNewlines(content, COLLAPSE_LINE_THRESHOLD) > COLLAPSE_LINE_THRESHOLD;
    return {
      collapsible: needsCollapse,
      preview: needsCollapse ? buildPreview(content) : content,
    };
  }, [content]);

  return (
    <div
      className="pl-3 border-l-2 text-sm font-mono leading-relaxed whitespace-pre-wrap break-words"
      style={{ borderLeftColor: accent, color: textColor }}
    >
      {collapsible && !expanded ? preview : content}
      {collapsible && (
        <button
          type="button"
          className="block mt-1 text-xs font-mono underline decoration-dotted cursor-pointer"
          style={{ color: 'var(--grey1)' }}
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? 'Show less' : `Show more (${(content.length / 1024).toFixed(1)} KB)`}
        </button>
      )}
    </div>
  );
}

export const CollapsiblePayload = memo(CollapsiblePayloadImpl);
