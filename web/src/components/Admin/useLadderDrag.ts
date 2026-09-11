import { useCallback, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent, RefObject } from 'react';
import type { SelectorLadders, SelectorRole, SelectorTier } from '../../types';
import { AXIS_MAX, BAR_STEP, barRange, cloneLadders, railValue, round3 } from './ladder';

export interface DragTarget {
  role: SelectorRole;
  tier: SelectorTier;
}

export interface HandleProps {
  onPointerDown: (e: ReactPointerEvent<HTMLElement>) => void;
  onPointerMove: (e: ReactPointerEvent<HTMLElement>) => void;
  onPointerUp: (e: ReactPointerEvent<HTMLElement>) => void;
  onPointerCancel: (e: ReactPointerEvent<HTMLElement>) => void;
  onKeyDown: (e: ReactKeyboardEvent<HTMLElement>) => void;
}

/** Arrow keys that step a bar, and the direction each one steps in. */
const KEY_DIRECTION: Record<string, number> = { ArrowUp: 1, ArrowRight: 1, ArrowDown: -1, ArrowLeft: -1 };

interface UseLadderDragOptions {
  ladders: SelectorLadders;
  linked: boolean;
  floor: number;
  /** The element whose box is the rail: pointer y maps onto its height. */
  railRef: RefObject<HTMLElement | null>;
  onChange: (next: SelectorLadders) => void;
}

interface UseLadderDragResult {
  dragging: DragTarget | null;
  handleProps: (role: SelectorRole, tier: SelectorTier) => HandleProps;
}

/**
 * Pointer-capture drag for the ladder handles. Capture is taken on the
 * handle element, so the caller must keep handles mounted for the whole
 * drag. Every move computes an absolute bar from the pointer's y, so a
 * stale closure between a change and its re-render is harmless: the next
 * move overwrites it. A linked drag writes the same bar into both ladders,
 * clamped to the range both accept; an unlinked drag touches only the
 * handle's own ladder. The arrow keys, Home and End reach the same writer,
 * so a keyboard user gets the identical clamp and linked semantics.
 */
export function useLadderDrag({ ladders, linked, floor, railRef, onChange }: UseLadderDragOptions): UseLadderDragResult {
  const [dragging, setDragging] = useState<DragTarget | null>(null);
  const activeRef = useRef<DragTarget | null>(null);
  const rectRef = useRef<DOMRect | null>(null);

  const applyValue = useCallback(
    (target: DragTarget, wanted: number) => {
      const roles: SelectorRole[] = linked ? ['coder', 'reviewer'] : [target.role];
      const range = barRange(ladders, roles, target.tier, floor);
      if (!range) return;
      const value = round3(Math.max(range[0], Math.min(range[1], wanted)));
      if (roles.every((r) => Math.abs(ladders[r][target.tier] - value) < 1e-9)) return;
      const next = cloneLadders(ladders);
      for (const r of roles) next[r][target.tier] = value;
      onChange(next);
    },
    [ladders, linked, floor, onChange],
  );

  const moveTo = useCallback(
    (clientY: number) => {
      const target = activeRef.current;
      const rect = rectRef.current;
      if (!target || !rect || rect.height <= 0) return;
      applyValue(target, railValue((clientY - rect.top) / rect.height));
    },
    [applyValue],
  );

  const endDrag = useCallback((e: ReactPointerEvent<HTMLElement>) => {
    if (!activeRef.current) return;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* the browser already released capture on cancel */
    }
    activeRef.current = null;
    rectRef.current = null;
    setDragging(null);
  }, []);

  const handleProps = useCallback(
    (role: SelectorRole, tier: SelectorTier): HandleProps => ({
      onPointerDown: (e) => {
        if (e.pointerType === 'mouse' && e.button !== 0) return;
        const rail = railRef.current;
        if (!rail) return;
        e.preventDefault();
        e.currentTarget.setPointerCapture(e.pointerId);
        rectRef.current = rail.getBoundingClientRect();
        activeRef.current = { role, tier };
        setDragging({ role, tier });
      },
      onPointerMove: (e) => {
        if (!activeRef.current) return;
        moveTo(e.clientY);
      },
      onPointerUp: endDrag,
      onPointerCancel: endDrag,
      onKeyDown: (e) => {
        const direction = KEY_DIRECTION[e.key];
        if (direction !== undefined) {
          e.preventDefault();
          applyValue({ role, tier }, round3(ladders[role][tier] + direction * BAR_STEP));

          return;
        }

        // Home and End ask for the extremes of the bar domain; the clamp in
        // applyValue turns them into the ends of this bar's own range.
        if (e.key === 'Home' || e.key === 'End') {
          e.preventDefault();
          applyValue({ role, tier }, e.key === 'Home' ? 0 : AXIS_MAX);
        }
      },
    }),
    [railRef, moveTo, endDrag, applyValue, ladders],
  );

  return { dragging, handleProps };
}
