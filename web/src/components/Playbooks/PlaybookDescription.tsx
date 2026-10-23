import { useLayoutEffect, useRef, useState } from 'react';
import { ChatMarkdown } from '../ChatPanel/ChatMarkdown';

interface PlaybookDescriptionProps {
  description: string;
  onStartEdit: () => void;
}

// Markup that acts on its own click: links, the code-block copy control, <details>.
const OWN_CLICK = 'a, button, summary, input, .copied';

/**
 * The description as markdown, clicked to edit. The clamp is visual, not a
 * string cut, so a list or fenced block never splits; overflow is measured
 * because soft line breaks make the source a poor guide to rendered height.
 * The caller keys it on the text, so a new description starts collapsed.
 */
export function PlaybookDescription({ description, onStartEdit }: PlaybookDescriptionProps) {
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const box = boxRef.current;
    const content = contentRef.current;
    if (expanded || !box || !content) return;
    const measure = () => setOverflows(box.scrollHeight > box.clientHeight);
    measure();
    // The markdown chunk loads lazily and the column reflows with the window.
    const observer = new ResizeObserver(measure);
    observer.observe(content);
    return () => observer.disconnect();
  }, [expanded]);

  return (
    <div className="mb-4">
      <div
        ref={boxRef}
        data-testid="playbook-description"
        onClick={(e) => {
          if ((e.target as Element).closest(OWN_CLICK) || window.getSelection()?.isCollapsed === false) return;
          onStartEdit();
        }}
        className="pb-description relative cursor-text"
        style={expanded ? undefined : { maxHeight: '8rem', overflow: 'hidden' }}
      >
        <div ref={contentRef}>
          <ChatMarkdown source={description} />
        </div>
        {!expanded && overflows && (
          <div
            aria-hidden="true"
            className="absolute inset-x-0 bottom-0 h-8 pointer-events-none"
            style={{ background: 'linear-gradient(transparent, var(--bg-dim))' }}
          />
        )}
      </div>
      {overflows && (
        <button
          type="button"
          aria-expanded={expanded}
          className="block mt-1 text-xs font-mono underline decoration-dotted cursor-pointer"
          style={{ color: 'var(--grey1)' }}
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? 'Show less' : 'Show more'}
        </button>
      )}
    </div>
  );
}
