import { Suspense, lazy, useEffect, useRef, useState } from 'react';
import { useTheme } from '../../hooks/useTheme';

// Lazy-load MDEditor so the ~5 MB editor chunk ships as its own bundle
// and is only fetched when a CardPanel is actually opened.
const MDEditor = lazy(() => import('@uiw/react-md-editor'));

// Approximate height above editor on mobile: header + title + type/priority +
// agent + description label + spacing.
const MOBILE_ABOVE_EDITOR_PX = 280;
// Panel switches to full-width below this breakpoint (matches .card-panel CSS).
const MOBILE_BREAKPOINT = 1024;
const DEFAULT_EDITOR_HEIGHT = 250;

function isMobileLayout(): boolean {
  return window.innerWidth <= MOBILE_BREAKPOINT;
}

// VisualViewport.height shrinks when the on-screen keyboard appears, giving
// the precise usable height above the keyboard.
function computeMobileEditorHeight(): number {
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  return Math.max(120, vvh - MOBILE_ABOVE_EDITOR_PX);
}

interface CardPanelEditorProps {
  body: string;
  onChange: (body: string) => void;
  collapsed: boolean;
  onToggleCollapsed: () => void;
}

/**
 * Markdown description editor for the CardPanel. Owns:
 *  - MDEditor embedding with theme-aware `data-color-mode`
 *  - Collapsible chevron toggle
 *  - Mobile/desktop height switching via VisualViewport
 *  - Cursor-follow scroll so typing past the visible bottom stays in view
 */
export function CardPanelEditor({
  body,
  onChange,
  collapsed,
  onToggleCollapsed,
}: CardPanelEditorProps) {
  const { theme } = useTheme();
  const editorContainerRef = useRef<HTMLDivElement>(null);
  const [editorHeight, setEditorHeight] = useState<number>(
    isMobileLayout() ? computeMobileEditorHeight() : DEFAULT_EDITOR_HEIGHT,
  );

  // Dynamically resize the editor when the visual viewport changes (e.g.
  // on-screen keyboard appearing/disappearing on mobile). On desktop the editor
  // keeps its default fixed height.
  useEffect(() => {
    function updateHeight() {
      if (isMobileLayout()) {
        setEditorHeight(computeMobileEditorHeight());
      } else {
        setEditorHeight(DEFAULT_EDITOR_HEIGHT);
      }
    }

    window.visualViewport?.addEventListener('resize', updateHeight);
    window.addEventListener('resize', updateHeight);
    updateHeight();

    return () => {
      window.visualViewport?.removeEventListener('resize', updateHeight);
      window.removeEventListener('resize', updateHeight);
    };
  }, []);

  // Auto-scroll the editor textarea so the cursor line stays visible when
  // typing past the bottom of the visible editor area.
  useEffect(() => {
    const container = editorContainerRef.current;
    if (!container) return;

    let textarea: HTMLTextAreaElement | null = null;

    function findTextarea() {
      textarea = container?.querySelector<HTMLTextAreaElement>(
        '.w-md-editor-text-input',
      ) ?? null;
      return textarea;
    }

    function handleInput() {
      if (!textarea) findTextarea();
      if (!textarea) return;

      const { selectionEnd, value } = textarea;
      const textBeforeCursor = value.slice(0, selectionEnd);
      const linesBefore = textBeforeCursor.split('\n').length;

      const computedStyle = window.getComputedStyle(textarea);
      const lineHeight = parseFloat(computedStyle.lineHeight) || 20;
      const paddingTop = parseFloat(computedStyle.paddingTop) || 0;

      const cursorY = paddingTop + (linesBefore - 1) * lineHeight;

      const visibleBottom = textarea.scrollTop + textarea.clientHeight;
      if (cursorY + lineHeight > visibleBottom) {
        textarea.scrollTop = cursorY + lineHeight - textarea.clientHeight + lineHeight;
      } else if (cursorY < textarea.scrollTop) {
        textarea.scrollTop = Math.max(0, cursorY - lineHeight);
      }
    }

    // Delay query so MDEditor has time to render its textarea.
    const timer = setTimeout(() => {
      findTextarea();
      if (textarea) {
        textarea.addEventListener('input', handleInput);
      }
    }, 100);

    return () => {
      clearTimeout(timer);
      textarea?.removeEventListener('input', handleInput);
    };
  }, []);

  return (
    <div ref={editorContainerRef} data-color-mode={theme}>
      <div className="flex items-center gap-1 mb-1">
        <span className="text-xs text-[var(--grey1)]">Description</span>
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
      )}
    </div>
  );
}
