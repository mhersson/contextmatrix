import { useRef } from 'react';

interface PanelBackdropProps {
  onClose: () => void;
}

/**
 * Dimming backdrop for the card drawer. Closing requires BOTH mousedown and
 * mouseup to land on the backdrop - the standard modal pattern. Prevents two
 * accidental-dismiss classes: drag-out (press inside the drawer, e.g. during
 * text selection, release over the backdrop) and misplaced replayed clicks
 * when a long main-thread task shifts the drawer layout between press and
 * release. Touch still works via the browser's synthesized mouse events.
 */
export function PanelBackdrop({ onClose }: PanelBackdropProps) {
  const pressedRef = useRef(false);
  return (
    <div
      className="fixed inset-0 bg-black/50 z-40"
      aria-hidden="true"
      data-testid="card-panel-backdrop"
      onMouseDown={(e) => {
        pressedRef.current = e.target === e.currentTarget;
      }}
      onMouseUp={(e) => {
        const shouldClose = pressedRef.current && e.target === e.currentTarget;
        pressedRef.current = false;
        if (shouldClose) onClose();
      }}
    />
  );
}
