import { Suspense, lazy, useCallback, useEffect, useId, useRef, useState } from 'react';
import { useTheme } from '../../hooks/useTheme';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useCursorFollowScroll } from '../../hooks/useCursorFollowScroll';
import { api } from '../../api/client';

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
 * Paste + drag-drop image upload: in edit mode the MDEditor textarea accepts
 * pasted clipboard images and dropped files. Each one is uploaded via
 * `api.uploadImage` and the resulting `![](url)` snippet is spliced into the
 * controlled body at the textarea's current selection. Inline status banner
 * surfaces upload progress / error.
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

  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

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

  const uploadAndInsert = useCallback(
    async (file: File) => {
      setUploading(true);
      setUploadError(null);
      try {
        const result = await api.uploadImage(file);
        insertImageRef(result.url);
      } catch (err) {
        const message =
          err && typeof err === 'object' && 'error' in err && typeof (err as { error: unknown }).error === 'string'
            ? (err as { error: string }).error
            : 'Upload failed';
        setUploadError(message);
      } finally {
        setUploading(false);
      }
    },
    [insertImageRef],
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const items = e.clipboardData?.items;
      if (!items) return;
      const files: File[] = [];
      for (const item of Array.from(items)) {
        if (item.kind === 'file' && item.type.startsWith('image/')) {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
      if (files.length === 0) return;
      e.preventDefault();
      for (const f of files) {
        void uploadAndInsert(f);
      }
    },
    [uploadAndInsert],
  );

  const handleDragOver = useCallback((e: React.DragEvent<HTMLDivElement>) => {
    // Block the browser's default "open file" navigation only when image
    // files are being dragged. Non-image drags fall through.
    if (e.dataTransfer?.types?.includes('Files')) {
      e.preventDefault();
    }
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent<HTMLDivElement>) => {
      const files = Array.from(e.dataTransfer?.files ?? []).filter((f) => f.type.startsWith('image/'));
      if (files.length === 0) return;
      e.preventDefault();
      for (const f of files) {
        void uploadAndInsert(f);
      }
    },
    [uploadAndInsert],
  );

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
          <div onDragOver={handleDragOver} onDrop={handleDrop}>
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
                textareaProps={{ onPaste: handlePaste }}
                className="bf-mdeditor"
              />
            </Suspense>
            {(uploading || uploadError) && (
              <div
                role="status"
                aria-live="polite"
                className="mt-2 text-xs"
                style={{ color: uploadError ? 'var(--red)' : 'var(--grey2)' }}
              >
                {uploadError ? `Upload failed: ${uploadError}` : 'Uploading image…'}
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
