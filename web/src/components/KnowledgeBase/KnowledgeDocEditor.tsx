import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api/client';
import { useEditorHeight } from '../../hooks/useEditorHeight';
import { useTheme } from '../../hooks/useTheme';

const MDEditor = lazy(() => import('@uiw/react-md-editor'));

interface EditorProps {
  initialContent: string;
  onCancel: () => void;
  onSave: (content: string, signal: AbortSignal) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}

export function KnowledgeDocEditor({ initialContent, onCancel, onSave, onDirtyChange }: EditorProps) {
  const [content, setContent] = useState(initialContent);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const { theme } = useTheme();
  const editorHeight = useEditorHeight();
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  const handleSave = async () => {
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setSaving(true);
    setSaveError(null);
    try {
      await onSave(content, ac.signal);
    } catch (err) {
      if (ac.signal.aborted) return;
      setSaveError(errorMessage(err));
    } finally {
      if (!ac.signal.aborted) setSaving(false);
    }
  };

  const handleCancel = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    onCancel();
  };

  return (
    <section className="p-6 flex flex-col h-full" data-color-mode={theme}>
      <div className="flex justify-end gap-2 mb-3">
        <button
          type="button"
          onClick={handleCancel}
          className="px-3 py-1 text-sm rounded"
          style={{ border: '1px solid var(--bg3)', color: 'var(--fg)', backgroundColor: 'transparent' }}
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving}
          className="px-3 py-1 text-sm rounded disabled:opacity-50"
          style={{ backgroundColor: 'var(--blue)', color: 'var(--bg0)' }}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
      {saveError && (
        <p className="mb-3 text-sm" style={{ color: 'var(--red)' }}>
          Save failed: {saveError}
        </p>
      )}
      <div className="flex-1 min-h-0">
        <Suspense fallback={null}>
          <MDEditor
            value={content}
            onChange={(v) => {
              const next = v ?? '';
              setContent(next);
              onDirtyChange?.(next !== initialContent);
            }}
            preview="edit"
            height={editorHeight}
          />
        </Suspense>
      </div>
    </section>
  );
}
