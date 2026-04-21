import { useEffect, type RefObject } from 'react';

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Traps keyboard focus inside `ref` while `isOpen` is true.
 *
 * On activation, focus is moved to `initialFocusRef.current` when provided and
 * still connected to the DOM; otherwise focus falls back to the first
 * focusable element inside the dialog. Tab / Shift+Tab wraps within the
 * dialog. On deactivation, the previously focused element is restored.
 *
 * Use `initialFocusRef` when the first focusable is not the element that
 * should receive focus (e.g., a form field deeper in the dialog, or an input
 * with autoFocus semantics that the trap would otherwise override).
 */
export function useFocusTrap(
  ref: RefObject<HTMLElement | null>,
  isOpen: boolean,
  initialFocusRef?: RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!isOpen || !ref.current) return;
    const dialog = ref.current;
    const previouslyFocused = document.activeElement as HTMLElement;

    const initialTarget = initialFocusRef?.current;
    if (initialTarget && dialog.contains(initialTarget)) {
      initialTarget.focus();
    } else {
      const focusables = dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
      focusables[0]?.focus();
    }

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return;
      const currentFocusables = dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
      const currentFirst = currentFocusables[0];
      const currentLast = currentFocusables[currentFocusables.length - 1];

      if (e.shiftKey && document.activeElement === currentFirst) {
        e.preventDefault();
        currentLast?.focus();
      } else if (!e.shiftKey && document.activeElement === currentLast) {
        e.preventDefault();
        currentFirst?.focus();
      }
    };

    dialog.addEventListener('keydown', handleKeyDown);
    return () => {
      dialog.removeEventListener('keydown', handleKeyDown);
      previouslyFocused?.focus();
    };
  }, [isOpen, ref, initialFocusRef]);
}
