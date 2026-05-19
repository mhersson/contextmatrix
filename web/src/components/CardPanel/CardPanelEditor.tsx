import { Suspense, lazy, useCallback, useEffect, useId, useRef } from 'react';
import { useTheme } from '../../hooks/useTheme';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useCursorFollowScroll } from '../../hooks/useCursorFollowScroll';
import { useImageUpload } from '../../hooks/useImageUpload';

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
 * Paste / drag-drop / click-to-upload image handling lives in
 * `useImageUpload`. This component owns the cursor-aware splice (inserting
 * `![](url)` at the current selection) and renders the hidden file input +
 * status banner.
 *
 * The visible label is associated via `aria-labelledby` on a wrapping
 * `role="group"` element — MDEditor does not expose a way to set an `id` on
 * its internal textarea.
 */
export function CardPanelEditor({ body, onChange, editable, editing, onToggleEditing }: CardPanelEditorProps) {
  const { theme } = useTheme();
  const editorContainerRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const labelId = useId();
  const editorHeight = useEditorHeight();

  // Ref captures the latest body so async upload handlers always splice into
  // the current value even after intervening edits. Updated in an effect to
  // satisfy react-hooks/refs (no ref writes during render).
  const bodyRef = useRef(body);
  useEffect(() => {
    bodyRef.current = body;
  }, [body]);

  useCursorFollowScroll(editorContainerRef);

  const inEditMode = editable && editing;
  const showToggle = !!onToggleEditing && editable;

  // Inserts `![](url)` at the editor textarea's cursor position. Falls back
  // to appending when no textarea is mounted (e.g. while Suspense is still
  // resolving the MDEditor chunk).
  const insertImageRef = useCallback(
    (url: string) => {
      const snippet = `![](${url})`;
      const textarea = editorContainerRef.current?.querySelector<HTMLTextAreaElement>('textarea');
      const current = bodyRef.current;

      if (!textarea) {
        const sep = current.length === 0 || current.endsWith('\n') ? '' : '\n';
        onChange(`${current}${sep}${snippet}\n`);
        return;
      }

      const start = textarea.selectionStart ?? current.length;
      const end = textarea.selectionEnd ?? current.length;
      const next = current.slice(0, start) + snippet + current.slice(end);
      onChange(next);

      // Restore selection just past the inserted snippet on the next tick so
      // subsequent typing continues from a sensible position. queueMicrotask
      // runs after React's controlled re-render flushes the new value.
      queueMicrotask(() => {
        const t = editorContainerRef.current?.querySelector<HTMLTextAreaElement>('textarea');
        if (!t) return;
        const cursor = start + snippet.length;
        t.focus();
        t.setSelectionRange(cursor, cursor);
      });
    },
    [onChange],
  );

  const upload = useImageUpload(insertImageRef);

  return (
    <section ref={editorContainerRef} data-color-mode={theme}>
      <div className="flex items-center justify-between mb-2">
        <div id={labelId} className="section-eyebrow">
          Description
        </div>
        <div className="flex items-center gap-2">
          {inEditMode && (
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={upload.uploading}
              aria-label="Upload image"
              className="px-2 py-1 rounded bg-transparent border border-[var(--bg4)] text-[var(--grey2)] hover:text-[var(--fg)] hover:border-[var(--bg5)] hover:bg-[var(--bg2)] transition-colors text-xs disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Upload image
            </button>
          )}
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
      </div>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        multiple
        className="sr-only"
        onChange={upload.handleFileSelect}
        tabIndex={-1}
        aria-hidden="true"
      />
      <div role="group" aria-labelledby={labelId}>
        {inEditMode ? (
          <div onDragOver={upload.handleDragOver} onDrop={upload.handleDrop}>
            <Suspense
              fallback={
                <textarea
                  value={body}
                  onChange={(e) => onChange(e.target.value)}
                  onPaste={upload.handlePaste}
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
                textareaProps={{ onPaste: upload.handlePaste }}
                className="bf-mdeditor"
              />
            </Suspense>
            {(upload.uploading || upload.uploadError) && (
              <div
                role="status"
                aria-live="polite"
                className="mt-2 text-xs"
                style={{ color: upload.uploadError ? 'var(--red)' : 'var(--grey2)' }}
              >
                {upload.uploadError ? `Upload failed: ${upload.uploadError}` : 'Uploading image…'}
              </div>
            )}
          </div>
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
