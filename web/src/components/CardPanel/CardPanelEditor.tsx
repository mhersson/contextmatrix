import { useId, useRef } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useCursorFollowScroll } from '../../hooks/useCursorFollowScroll';

interface CardPanelEditorProps {
  value: string;
  onChange: (value: string) => void;
  collapsed: boolean;
  onToggleCollapsed: () => void;
}

/**
 * Markdown editor section inside the card panel. MDEditor does not expose a
 * way to set an `id` on its internal textarea, so the visible label is
 * associated via `aria-labelledby` on a wrapping `role="group"` element.
 */
export function CardPanelEditor({
  value,
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
        >
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d={collapsed ? 'M19 9l-7 7-7-7' : 'M5 15l7-7 7 7'}
            />
          </svg>
        </button>
      </div>
      {!collapsed && (
        <div role="group" aria-labelledby={labelId}>
          <MDEditor
            value={value}
            onChange={(val) => onChange(val || '')}
            preview="edit"
            height={editorHeight}
            visibleDragbar={false}
            previewOptions={{ skipHtml: true }}
          />
        </div>
      )}
    </div>
  );
}
