import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api/client';
import { useTheme } from '../../hooks/useTheme';

const MDEditor = lazy(() => import('@uiw/react-md-editor'));

interface EditorProps {
  initialContent: string;
  doc?: string;
  onCancel: () => void;
  onSave: (content: string, signal: AbortSignal) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
  onOpenSelector?: () => void;
}

const MIN_EDITOR_PX = 160;

export function KnowledgeDocEditor({ initialContent, doc, onCancel, onSave, onDirtyChange, onOpenSelector }: EditorProps) {
  const [content, setContent] = useState(initialContent);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const { theme } = useTheme();
  const abortRef = useRef<AbortController | null>(null);
  const fillRef = useRef<HTMLDivElement>(null);
  const [editorHeight, setEditorHeight] = useState<number>(400);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  // Fill the available column with the editor. ResizeObserver tracks the
  // wrapper's box; the visual viewport check shrinks the height when the
  // mobile keyboard opens so the editor stays above it.
  useEffect(() => {
    const el = fillRef.current;
    if (!el) return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      const vvh = window.visualViewport?.height ?? window.innerHeight;
      const usable = Math.max(MIN_EDITOR_PX, Math.min(rect.height, vvh - rect.top));
      setEditorHeight(Math.floor(usable));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener('resize', measure);
    window.visualViewport?.addEventListener('resize', measure);
    return () => {
      ro.disconnect();
      window.removeEventListener('resize', measure);
      window.visualViewport?.removeEventListener('resize', measure);
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
    <section className="flex flex-col h-full" data-color-mode={theme}>
      {onOpenSelector && (
        <button
          type="button"
          onClick={onOpenSelector}
          aria-label="Open document selector"
          className="md:hidden px-4 py-2 flex items-center gap-2 text-sm w-full text-left"
          style={{ borderBottom: '1px solid var(--bg3)', color: 'var(--fg)', backgroundColor: 'var(--bg0)' }}
        >
          {doc ? (
            <>
              <svg className="w-4 h-4 flex-shrink-0" style={{ color: 'var(--grey1)' }} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
                <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
              </svg>
              <span className="flex-1 truncate" aria-hidden="true">{doc}</span>
              <svg className="w-4 h-4 flex-shrink-0" style={{ color: 'var(--grey1)' }} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <polyline points="9 18 15 12 9 6" />
              </svg>
            </>
          ) : (
            <span style={{ color: 'var(--grey1)' }}>Choose a document ›</span>
          )}
        </button>
      )}
      <header
        className="flex items-center justify-between gap-3 px-6 py-3"
        style={{
          borderBottom: '1px solid var(--bg3)',
          backgroundColor: 'var(--bg-dim)',
        }}
      >
        <span className="section-eyebrow">Editing knowledge doc</span>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={handleCancel}
            className="px-3 py-1.5 rounded text-sm transition-colors"
            style={{
              border: '1px solid var(--bg3)',
              color: 'var(--fg)',
              backgroundColor: 'transparent',
            }}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSave}
            disabled={saving}
            className="px-3 py-1.5 rounded text-sm font-medium hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed"
            style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </header>
      {saveError && (
        <p
          role="alert"
          className="px-6 py-2 text-sm"
          style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
        >
          Save failed: {saveError}
        </p>
      )}
      <div ref={fillRef} className="flex-1 min-h-0">
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
            visibleDragbar={false}
          />
        </Suspense>
      </div>
    </section>
  );
}
