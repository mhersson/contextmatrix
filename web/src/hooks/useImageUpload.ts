import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';

/**
 * Encapsulates the paste / drop / file-select image upload flow used by the
 * card-panel editor. Returns the state surface (uploading + error) and the
 * four input-event handlers. The consumer owns the hidden `<input
 * type="file">` element and its ref, attaches `handleFileSelect` to that
 * input's `onChange`, and triggers the picker by calling `.click()` on its
 * own ref — keeping all ref handling outside the hook so the React refs
 * lint rule sees no ref reads through this object.
 *
 * Each upload is sent through `api.uploadImage` and the resulting `![](url)`
 * markdown is handed back to `onInsert`, which the caller is responsible for
 * splicing into its controlled body at the appropriate cursor position.
 *
 * The hook is intentionally framework-pure: it owns nothing visual, only the
 * upload protocol and minimal status. Components decide how to render the
 * banner and where to mount the hidden file input.
 */
export interface UseImageUpload {
  uploading: boolean;
  uploadError: string | null;
  handlePaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => void;
  handleDragOver: (e: React.DragEvent<HTMLDivElement>) => void;
  handleDrop: (e: React.DragEvent<HTMLDivElement>) => void;
  handleFileSelect: (e: React.ChangeEvent<HTMLInputElement>) => void;
}

export function useImageUpload(onInsert: (url: string) => void): UseImageUpload {
  // AbortController shared across the lifetime of the hook. Aborting it on
  // unmount cancels every fetch still in flight so onInsert / setState
  // never fire against a torn-down consumer. The bail-out branch in
  // `uploadAndInsert` covers paste/drop events fired after cleanup but
  // before the next render.
  //
  // The controller is constructed inside the effect (not at ref init) so
  // React 19 StrictMode's dev-only setup → cleanup → setup pair re-creates
  // it on the second setup. Initializing at `useRef` would leave the ref
  // stuck at `null` after the first cleanup nulled it out, silently
  // breaking every paste / drop / file-pick for the rest of the
  // component's life in dev.
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    abortRef.current = new AbortController();
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  // Track inflight count rather than a boolean so concurrent uploads (a
  // multi-file paste or drop) don't flicker the banner / re-enable the
  // upload button when one finishes while others are still in flight.
  const [inflight, setInflight] = useState(0);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const uploadAndInsert = useCallback(
    async (file: File) => {
      const controller = abortRef.current;
      if (!controller) return; // unmounted between handler entry and call
      setInflight((n) => n + 1);
      try {
        const result = await api.uploadImage(file, controller.signal);
        if (controller.signal.aborted) return;
        onInsert(result.url);
        // Clear stale error only on a successful upload — failures from
        // earlier files should not vanish silently when a later file
        // succeeds first, but a clean success means there's nothing to
        // report any more.
        setUploadError(null);
      } catch (err) {
        // Aborted fetches throw DOMException("AbortError"); swallow them
        // so the consumer doesn't see an "Upload failed" banner that
        // really meant "the user navigated away".
        if (controller.signal.aborted) return;
        if (err instanceof DOMException && err.name === 'AbortError') return;
        const message =
          err && typeof err === 'object' && 'error' in err && typeof (err as { error: unknown }).error === 'string'
            ? (err as { error: string }).error
            : 'Upload failed';
        setUploadError(message);
      } finally {
        if (!controller.signal.aborted) {
          setInflight((n) => n - 1);
        }
      }
    },
    [onInsert],
  );

  const uploading = inflight > 0;

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

  const handleFileSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? []).filter((f) => f.type.startsWith('image/'));
      // Reset the input value unconditionally so re-selecting the same file
      // re-fires onChange. Without this the second pick is a no-op.
      e.target.value = '';
      if (files.length === 0) return;
      for (const f of files) {
        void uploadAndInsert(f);
      }
    },
    [uploadAndInsert],
  );

  return {
    uploading,
    uploadError,
    handlePaste,
    handleDragOver,
    handleDrop,
    handleFileSelect,
  };
}
