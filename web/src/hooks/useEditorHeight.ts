import { useEffect, useState } from 'react';

// Approximate height in px of the panel content above the editor on mobile
// (header bar ~57px + title section ~60px + type/priority/state row ~60px +
// agent section ~50px + description label ~20px + spacing ~33px).
const MOBILE_ABOVE_EDITOR_PX = 280;

// Panel switches to full-width mode at this breakpoint (matches .card-panel CSS).
const MOBILE_BREAKPOINT = 1024;

const DEFAULT_EDITOR_HEIGHT = 375;

/** True when the panel occupies the full viewport width. */
function isMobileLayout(): boolean {
  if (typeof window === 'undefined') return false;
  return window.innerWidth <= MOBILE_BREAKPOINT;
}

/**
 * Computes the editor height for mobile using the VisualViewport API.
 * VisualViewport.height shrinks when the on-screen keyboard appears, giving us
 * the precise usable height above the keyboard without any extra calculation.
 */
function computeMobileEditorHeight(): number {
  if (typeof window === 'undefined') return DEFAULT_EDITOR_HEIGHT;
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  return Math.max(120, vvh - MOBILE_ABOVE_EDITOR_PX);
}

/**
 * Returns a dynamically-resizing editor height. On mobile it follows the
 * VisualViewport height (shrinks when the soft keyboard appears). On desktop
 * it stays at DEFAULT_EDITOR_HEIGHT.
 */
export function useEditorHeight(): number {
  const [editorHeight, setEditorHeight] = useState<number>(() =>
    isMobileLayout() ? computeMobileEditorHeight() : DEFAULT_EDITOR_HEIGHT,
  );

  useEffect(() => {
    function updateHeight() {
      if (isMobileLayout()) {
        setEditorHeight(computeMobileEditorHeight());
      } else {
        setEditorHeight(DEFAULT_EDITOR_HEIGHT);
      }
    }

    // VisualViewport fires 'resize' when the keyboard opens/closes on mobile.
    window.visualViewport?.addEventListener('resize', updateHeight);
    // Also listen to window resize for orientation changes and desktop resizes.
    window.addEventListener('resize', updateHeight);

    // Set initial height based on current state.
    updateHeight();

    return () => {
      window.visualViewport?.removeEventListener('resize', updateHeight);
      window.removeEventListener('resize', updateHeight);
    };
  }, []);

  return editorHeight;
}
