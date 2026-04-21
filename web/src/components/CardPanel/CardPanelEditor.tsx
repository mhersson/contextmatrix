import { Suspense, lazy, useId, useRef } from 'react';
import { useTheme } from '../../hooks/useTheme';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useCursorFollowScroll } from '../../hooks/useCursorFollowScroll';

// Lazy-load MDEditor so the ~5 MB editor chunk ships as its own bundle
// and is only fetched when a CardPanel is actually opened.
const MDEditor = lazy(() => import('@uiw/react-md-editor'));

interface CardPanelEditorProps {
  body: string;
  onChange: (body: string) => void;
  collapsed: boolean;
  onToggleCollapsed: () => void;
}

/**
 * Markdown description editor for the CardPanel. Owns:
 *  - MDEditor embedding with theme-aware `data-color-mode`
 *  - Collapsible chevron toggle with aria-expanded
 *  - Mobile/desktop height switching via VisualViewport (useEditorHeight)
 *  - Cursor-follow scroll so typing past the visible bottom stays in view
 *    (useCursorFollowScroll)
 *
 * MDEditor does not expose a way to set an `id` on its internal textarea, so
 * the visible label is associated via `aria-labelledby` on a wrapping
 * `role="group"` element.
 */
export function CardPanelEditor({
  body,
  onChange,
  collapsed,
  onToggleCollapsed,
}: CardPanelEditorProps) {
  const { theme } = useTheme();
  const editorContainerRef = useRef<HTMLDivElement>(null);
  const labelId = useId();
  const editorHeight = useEditorHeight();

  useCursorFollowScroll(editorContainerRef);

  return (
    <div ref={editorContainerRef} data-color-mode={theme}>
      <div className="flex items-center gap-1 mb-1">
        <span id={labelId} className="text-xs text-[var(--grey1)]">
          Description
        </span>
        <button
          onClick={onToggleCollapsed}
          className="flex items-center justify-center text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
          aria-label={collapsed ? 'Expand description' : 'Collapse description'}
          aria-expanded={!collapsed}
        >
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
              d={collapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'} />
          </svg>
        </button>
      </div>
      {!collapsed && (
        <div role="group" aria-labelledby={labelId}>
          <Suspense
            fallback={
              <textarea
                value={body}
                onChange={(e) => onChange(e.target.value)}
                style={{ height: editorHeight }}
                className="w-full p-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] font-mono resize-none focus:outline-none focus:border-[var(--aqua)]"
                aria-label="Description (loading rich editor...)"
              />
            }
          >
            <MDEditor
              value={body}
              onChange={(val) => onChange(val || '')}
              preview="edit"
              height={editorHeight}
              visibleDragbar={false}
              previewOptions={{ skipHtml: true }}
            />
          </Suspense>
        </div>
      )}
    </div>
  );
}
