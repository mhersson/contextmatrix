import { useEffect, type RefObject } from 'react';

/**
 * Keeps the MDEditor textarea cursor line visible when typing past the bottom
 * of the visible editor area. Targets the `.w-md-editor-text-input` textarea
 * inside the referenced container.
 */
export function useCursorFollowScroll(
  editorContainerRef: RefObject<HTMLDivElement | null>,
): void {
  useEffect(() => {
    const container = editorContainerRef.current;
    if (!container) return;

    // The MDEditor renders a hidden textarea that receives keyboard input.
    // We wait a tick for the editor to finish mounting before querying it.
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

      // Compute the cursor's approximate vertical position within the textarea
      // by measuring how many lines precede the cursor and multiplying by the
      // computed line height.
      const { selectionEnd, value } = textarea;
      const textBeforeCursor = value.slice(0, selectionEnd);
      const linesBefore = textBeforeCursor.split('\n').length;

      const computedStyle = window.getComputedStyle(textarea);
      const lineHeight = parseFloat(computedStyle.lineHeight) || 20;
      const paddingTop = parseFloat(computedStyle.paddingTop) || 0;

      const cursorY = paddingTop + (linesBefore - 1) * lineHeight;

      // Scroll so the cursor line is visible, keeping one extra line of context.
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
  }, [editorContainerRef]);
}
