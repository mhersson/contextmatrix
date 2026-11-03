import { useCallback, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent, RefObject } from 'react';
import type { SelectorLadders, SelectorRole, SelectorTier } from '../../types';
import { AXIS_MAX, BAR_STEP, barRange, cloneLadders, round3, snapBar } from './ladder';

export interface DragTarget {
  role: SelectorRole;
  tier: SelectorTier;
}

export interface HandleProps {
  onPointerDown: (e: ReactPointerEvent<Element>) => void;
  onPointerMove: (e: ReactPointerEvent<Element>) => void;
  onPointerUp: (e: ReactPointerEvent<Element>) => void;
  onPointerCancel: (e: ReactPointerEvent<Element>) => void;
  onKeyDown: (e: ReactKeyboardEvent<Element>) => void;
}

/** Arrow keys that step a bar, and the direction each one steps in. */
const KEY_DIRECTION: Record<string, number> = { ArrowUp: 1, ArrowRight: 1, ArrowDown: -1, ArrowLeft: -1 };

interface UseLadderDragOptions {
  ladders: SelectorLadders;
  linked: boolean;
  floor: number;
  /** Element whose box is measured at pointer down. */
  targetRef: RefObject<Element | null>;
  /** Prior change for a pointer travel of dy client px (down is positive) over an element of this box. */
  valueDelta: (dy: number, rect: DOMRect) => number;
  onChange: (next: SelectorLadders) => void;
}

interface UseLadderDragResult {
  dragging: DragTarget | null;
  handleProps: (role: SelectorRole, tier: SelectorTier) => HandleProps;
}

/**
 * Pointer-capture drag for the ladder handles. Capture is taken on the
 * handle element, so the caller must keep handles mounted for the whole
 * drag. Every move computes the bar from its value at the grab plus the
 * pointer's travel since the grab, so a handle drawn offset from its line
 * never jumps and a stale closure between a change and its re-render is
 * harmless: the next move overwrites it. A linked drag writes the same bar
 * into both ladders, clamped to the range both accept; an unlinked drag
 * touches only the handle's own ladder. The arrow keys, Home and End reach
 * the same writer, so a keyboard user gets the identical clamp and linked
 * semantics.
 */
export function useLadderDrag({ ladders, linked, floor, targetRef, valueDelta, onChange }: UseLadderDragOptions): UseLadderDragResult {
  const [dragging, setDragging] = useState<DragTarget | null>(null);
  const activeRef = useRef<DragTarget | null>(null);
  const grabRef = useRef<{ rect: DOMRect; y: number; value: number } | null>(null);

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
      const grab = grabRef.current;
      if (!target || !grab || grab.rect.height <= 0) return;
      applyValue(target, snapBar(grab.value + valueDelta(clientY - grab.y, grab.rect)));
    },
    [applyValue, valueDelta],
  );

  const endDrag = useCallback((e: ReactPointerEvent<Element>) => {
    if (!activeRef.current) return;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* the browser already released capture on cancel */
    }
    activeRef.current = null;
    grabRef.current = null;
    setDragging(null);
  }, []);

  const handleProps = useCallback(
    (role: SelectorRole, tier: SelectorTier): HandleProps => ({
      onPointerDown: (e) => {
        if (e.pointerType === 'mouse' && e.button !== 0) return;
        const el = targetRef.current;
        if (!el) return;
        e.preventDefault();
        e.currentTarget.setPointerCapture(e.pointerId);
        grabRef.current = { rect: el.getBoundingClientRect(), y: e.clientY, value: ladders[role][tier] };
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
    [targetRef, moveTo, endDrag, applyValue, ladders],
  );

  return { dragging, handleProps };
}
