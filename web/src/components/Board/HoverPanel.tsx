import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject, type SyntheticEvent } from 'react';
import { createPortal } from 'react-dom';

const GAP = 8;
const stop = (e: SyntheticEvent) => e.stopPropagation();
// Also keeps focus on the trigger: a press inside does not blur-close the
// panel, and Escape still reaches the trigger.
const stopAndKeepFocus = (e: SyntheticEvent) => {
  e.stopPropagation();
  e.preventDefault();
};

interface HoverPanelProps {
  /** DOM id the trigger references via aria-describedby. */
  id: string;
  /** Element the panel floats next to; a null current skips positioning. */
  anchorRef: RefObject<HTMLElement | null>;
  /** Receives the panel element, for a trigger that needs to tell presses inside it apart. */
  panelRef?: RefObject<HTMLDivElement | null>;
  width: number;
  /**
   * `beside`: right of the anchor, flipped left at the viewport edge (the
   * card hovercard). `below`: under the anchor, left-aligned, flipped above
   * when there is no room below (small triggers inside a card).
   */
  placement?: 'beside' | 'below';
  className?: string;
  /**
   * Given, the panel takes the pointer (`.card-hover--hoverable`), so it can
   * be moved onto and read without closing (WCAG 1.4.13); the trigger keeps
   * it open while the pointer is over it.
   */
  onPointerInside?: (inside: boolean) => void;
  children: ReactNode;
}

/**
 * The board's floating hover panel (`.card-hover`): a `role="tooltip"`
 * rendered through a portal so column scroll containers cannot clip it, and
 * kept inside the viewport. The trigger owns opening, closing and
 * aria-describedby.
 */
export function HoverPanel({ id, anchorRef, panelRef: outerRef, width, placement = 'beside', className = '', onPointerInside, children }: HoverPanelProps) {
  const ownRef = useRef<HTMLDivElement>(null);
  const panelRef = outerRef ?? ownRef;
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);

  // Layout effect: the (0,0) first render is measured and moved before paint.
  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    if (!anchor || !panelRef.current) return;
    const rect = anchor.getBoundingClientRect();
    const height = panelRef.current.offsetHeight;
    if (placement === 'below') {
      const left = Math.max(GAP, Math.min(rect.left, window.innerWidth - width - GAP));
      const fitsBelow = rect.bottom + GAP + height <= window.innerHeight - GAP;
      const top = fitsBelow ? rect.bottom + GAP : Math.max(GAP, rect.top - GAP - height);
      setPos({ top, left });
      return;
    }
    const fitsRight = rect.right + GAP + width <= window.innerWidth;
    const left = fitsRight ? rect.right + GAP : Math.max(GAP, rect.left - GAP - width);
    const top = Math.max(GAP, Math.min(rect.top, window.innerHeight - height - GAP));
    setPos({ top, left });
  }, [anchorRef, panelRef, placement, width]);

  return createPortal(
    <div
      ref={panelRef}
      id={id}
      role="tooltip"
      className={`card-hover ${onPointerInside ? 'card-hover--hoverable' : ''} ${className}`.replace(/\s+/g, ' ').trim()}
      style={{ width, top: pos?.top ?? 0, left: pos?.left ?? 0 }}
      onMouseEnter={onPointerInside && (() => onPointerInside(true))}
      onMouseLeave={onPointerInside && (() => onPointerInside(false))}
      // React events bubble through the portal to the trigger's ancestors: a
      // press inside a hoverable panel must not open or start dragging the card.
      onClick={onPointerInside && stop}
      onPointerDown={onPointerInside && stop}
      onMouseDown={onPointerInside && stopAndKeepFocus}
    >
      {children}
    </div>,
    document.body,
  );
}
