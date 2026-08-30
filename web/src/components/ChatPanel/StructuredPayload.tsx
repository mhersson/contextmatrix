import { memo, useMemo, useState } from 'react';
import {
  COLLAPSE_CHAR_THRESHOLD,
  COLLAPSE_LINE_THRESHOLD,
  JSON_PREVIEW_CHAR_LIMIT,
  JSON_PREVIEW_LINE_LIMIT,
  countNewlines,
} from './chatEntryUtils';

interface StructuredPayloadProps {
  /** Pretty-printed JSON, from parseStructured. */
  pretty: string;
}

/** Cut the preview at JSON_PREVIEW_LINE_LIMIT lines within
 *  JSON_PREVIEW_CHAR_LIMIT characters. The STRING is truncated (not
 *  CSS-clamped) so a large payload never enters layout while collapsed. */
function buildPreview(pretty: string): string {
  const preview = pretty.slice(0, JSON_PREVIEW_CHAR_LIMIT);
  let idx = -1;
  for (let n = 0; n < JSON_PREVIEW_LINE_LIMIT; n++) {
    idx = preview.indexOf('\n', idx + 1);
    if (idx === -1) return preview;
  }
  return preview.slice(0, idx);
}

/**
 * Structured JSON message body (planner output, mob-moderator verdicts)
 * rendered as a code block. Long payloads collapse to the first few lines with
 * a Read more toggle; expansion state is local and ephemeral, like
 * CollapsiblePayload's.
 */
function StructuredPayloadImpl({ pretty }: StructuredPayloadProps) {
  const [expanded, setExpanded] = useState(false);

  const { collapsible, preview } = useMemo(() => {
    const needsCollapse =
      pretty.length > COLLAPSE_CHAR_THRESHOLD ||
      countNewlines(pretty, COLLAPSE_LINE_THRESHOLD) > COLLAPSE_LINE_THRESHOLD;
    return {
      collapsible: needsCollapse,
      preview: needsCollapse ? buildPreview(pretty) : pretty,
    };
  }, [pretty]);

  return (
    <div>
      <pre
        data-testid="structured-payload"
        className="rounded px-2 py-1.5 text-xs font-mono leading-relaxed whitespace-pre-wrap wrap-anywhere max-w-full"
        style={{
          backgroundColor: 'var(--bg1)',
          border: '1px solid var(--bg3)',
          color: 'var(--fg)',
        }}
      >
        <code>{collapsible && !expanded ? preview : pretty}</code>
      </pre>
      {collapsible && (
        <button
          type="button"
          aria-expanded={expanded}
          className="block mt-1 text-xs font-mono underline decoration-dotted cursor-pointer"
          style={{ color: 'var(--grey1)' }}
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? 'Show less' : `Read more (${(pretty.length / 1024).toFixed(1)} KB)`}
        </button>
      )}
    </div>
  );
}

export const StructuredPayload = memo(StructuredPayloadImpl);
