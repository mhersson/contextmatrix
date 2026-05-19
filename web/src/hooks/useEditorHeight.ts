import { useEffect, useState } from 'react';

// Approximate height in px of the panel content above the editor on mobile
// (header bar ~57px + title section ~60px + type/priority/state row ~60px +
// agent section ~50px + description label ~20px + spacing ~33px).
const MOBILE_ABOVE_EDITOR_PX = 280;

// Small reserve when the soft keyboard is open: editor toolbar + description
// label that stay visible above the keyboard (~60px).
const KEYBOARD_OPEN_RESERVE = 60;

// Panel switches to full-width mode at this breakpoint (matches .card-panel CSS).
const MOBILE_BREAKPOINT = 1024;

const DEFAULT_EDITOR_HEIGHT = 375;

/** True when the panel occupies the full viewport width. */
function isMobileLayout(): boolean {
  if (typeof window === 'undefined') return false;
  return window.innerWidth <= MOBILE_BREAKPOINT;
}

/**
 * Returns true when iOS/Android has opened the on-screen keyboard.
 * We detect it by comparing visualViewport.height to window.innerHeight —
 * the viewport shrinks when the keyboard is shown. A 100px tolerance avoids
 * false-positives from ordinary browser-chrome adjustments.
 */
function isKeyboardOpen(): boolean {
  if (typeof window === 'undefined') return false;
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  return vvh < window.innerHeight - 100;
}

/**
 * Computes the editor height for mobile using the VisualViewport API.
 *
 * When the soft keyboard is open, iOS scrolls the focused element into view so
 * the content above the editor is no longer visible — subtracting
 * MOBILE_ABOVE_EDITOR_PX would over-correct. Instead we use a small
 * KEYBOARD_OPEN_RESERVE covering only what remains visible (toolbar + label).
 *
 * A floor of 50% of window.innerHeight is applied in both keyboard states so
 * the editor is never less than half the physical screen height on mobile.
 */
function computeMobileEditorHeight(): number {
  if (typeof window === 'undefined') return DEFAULT_EDITOR_HEIGHT;
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  const halfScreen = window.innerHeight * 0.5;
  if (isKeyboardOpen()) {
    return Math.max(vvh - KEYBOARD_OPEN_RESERVE, halfScreen);
  }
  return Math.max(vvh - MOBILE_ABOVE_EDITOR_PX, halfScreen);
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
