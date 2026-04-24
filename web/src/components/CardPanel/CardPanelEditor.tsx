import { Suspense, lazy, useId, useRef } from 'react';
import { useTheme } from '../../hooks/useTheme';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useCursorFollowScroll } from '../../hooks/useCursorFollowScroll';

const MDEditor = lazy(() => import('@uiw/react-md-editor'));
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

interface CardPanelEditorProps {
  body: string;
  onChange: (body: string) => void;
  editable: boolean;
  editing: boolean;
  onToggleEditing?: () => void;
}

/**
 * Description surface for the CardPanel left column.
 *
 * - `editable` gates whether edits are possible at all (false when the runner
 *   is attached).
 * - `editing` is the user-controlled toggle; only when `editable && editing`
 *   is MDEditor mounted. Otherwise the body renders through
 *   `@uiw/react-markdown-preview` so the read-only view has no editor chrome
 *   and flows into the panel as plain content.
 * - `onToggleEditing` is optional: when provided, an "Open in editor" /
 *   "Close editor" button appears next to the Description eyebrow. Callers
 *   that always want the editor open (CreateCardPanel) omit it.
 *
 * The visible label is associated via `aria-labelledby` on a wrapping
 * `role="group"` element — MDEditor does not expose a way to set an `id` on
 * its internal textarea.
 */
export function CardPanelEditor({ body, onChange, editable, editing, onToggleEditing }: CardPanelEditorProps) {
  const { theme } = useTheme();
  const editorContainerRef = useRef<HTMLDivElement>(null);
  const labelId = useId();
  const editorHeight = useEditorHeight();

  useCursorFollowScroll(editorContainerRef);

  const inEditMode = editable && editing;
  const showToggle = !!onToggleEditing && editable;

  return (
    <section ref={editorContainerRef} data-color-mode={theme}>
      <div className="flex items-center justify-between mb-2">
        <div id={labelId} className="section-eyebrow">
          Description
        </div>
        {showToggle && (
          <button
            type="button"
            onClick={onToggleEditing}
            className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs"
          >
            {editing ? 'Close editor' : 'Open in editor'}
          </button>
        )}
      </div>
      <div role="group" aria-labelledby={labelId}>
        {inEditMode ? (
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
              hideToolbar={false}
              height={editorHeight}
              visibleDragbar
              previewOptions={{ skipHtml: true }}
              className="bf-mdeditor"
            />
          </Suspense>
        ) : (
          <Suspense
            fallback={
              <div
                className="bf-markdown-fallback whitespace-pre-wrap font-mono text-sm"
                style={{ color: 'var(--fg)' }}
              >
                {body}
              </div>
            }
          >
            <MarkdownPreview source={body} skipHtml className="bf-markdown" />
          </Suspense>
        )}
      </div>
    </section>
  );
}
