import { useEffect, useState } from 'react';

// Approximate height in px of the panel content above the editor on mobile
// (header bar ~57px + title section ~60px + type/priority/state row ~60px +
// agent section ~50px + description label ~20px + spacing ~33px).
const MOBILE_ABOVE_EDITOR_PX = 280;

// Small reserve when the soft keyboard is open: editor toolbar (~40px) +
// description label that stays visible above the keyboard (~20px).
const KEYBOARD_OPEN_RESERVE = 60;

// Panel switches to full-width mode at this breakpoint (matches .card-panel CSS).
const MOBILE_BREAKPOINT = 1024;

const DEFAULT_EDITOR_HEIGHT = 375;

// The viewport shrinks by more than this many px when the soft keyboard opens.
// A tolerance this large avoids false-positives from ordinary browser-chrome
// adjustments (e.g. hiding/showing the address bar).
const KEYBOARD_DETECTION_TOLERANCE_PX = 100;

// Floor multiplier: the editor is never less than this fraction of the
// physical screen height on mobile, regardless of keyboard state.
const MIN_EDITOR_FRACTION_OF_SCREEN = 0.5;

/** True when the panel occupies the full viewport width. */
function isMobileLayout(): boolean {
  if (typeof window === 'undefined') return false;
  return window.innerWidth <= MOBILE_BREAKPOINT;
}

/**
 * Computes the editor height for mobile using the VisualViewport API.
 *
 * Reads visualViewport.height once and uses the same snapshot for both the
 * keyboard-open detection and the height formula, eliminating any race between
 * two separate reads.
 *
 * When the soft keyboard is open, iOS scrolls the focused element into view so
 * the content above the editor is no longer visible - subtracting
 * MOBILE_ABOVE_EDITOR_PX would over-correct. Instead we use a small
 * KEYBOARD_OPEN_RESERVE covering only what remains visible (toolbar + label).
 *
 * A floor of MIN_EDITOR_FRACTION_OF_SCREEN of window.innerHeight is applied in
 * both keyboard states so the editor is never less than half the physical
 * screen height on mobile.
 */
function computeMobileEditorHeight(): number {
  if (typeof window === 'undefined') return DEFAULT_EDITOR_HEIGHT;
  const vvh = window.visualViewport?.height ?? window.innerHeight;
  const halfScreen = window.innerHeight * MIN_EDITOR_FRACTION_OF_SCREEN;
  const keyboardOpen = vvh < window.innerHeight - KEYBOARD_DETECTION_TOLERANCE_PX;
  if (keyboardOpen) {
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
      const next = isMobileLayout() ? computeMobileEditorHeight() : DEFAULT_EDITOR_HEIGHT;
      setEditorHeight((prev) => (prev === next ? prev : next));
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
