import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';

export interface ModelContextMenuProps {
  slug: string;
  /** Viewport coordinates of the pointer that opened the menu. */
  x: number;
  y: number;
  blacklisted: boolean;
  onBlacklist: (slug: string) => void;
  onDelist: (slug: string) => void;
  onClose: () => void;
}

/** Keeps the menu inside the viewport when it opens near an edge. */
const EDGE_GAP = 8;

/**
 * Right-click menu for one model on the ladders page: blacklist it, or delist
 * it when it already is. The pointer position is the anchor; the menu is
 * nudged back inside the viewport when it would overflow. The item takes
 * focus on open so Enter and Escape work at once.
 */
export function ModelContextMenu({ slug, x, y, blacklisted, onBlacklist, onDelist, onClose }: ModelContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null);
  const itemRef = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  useMenuDismiss(ref, true, onClose);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const { width, height } = el.getBoundingClientRect();
    const left = Math.max(EDGE_GAP, Math.min(x, window.innerWidth - width - EDGE_GAP));
    const top = Math.max(EDGE_GAP, Math.min(y, window.innerHeight - height - EDGE_GAP));
    setPos({ left, top });
  }, [x, y]);

  useEffect(() => {
    itemRef.current?.focus();
  }, []);

  const act = () => {
    if (blacklisted) onDelist(slug);
    else onBlacklist(slug);
    onClose();
  };

  return (
    <div ref={ref} role="menu" aria-label={slug} className="tl-menu" style={{ left: pos.left, top: pos.top }} data-testid="tl-menu">
      <div className="tl-menu-head" aria-hidden="true">
        {slug}
      </div>
      <button ref={itemRef} type="button" role="menuitem" className={`tl-menu-item${blacklisted ? '' : ' danger'}`} onClick={act}>
        {blacklisted ? 'Remove from blacklist' : 'Add to blacklist'}
      </button>
    </div>
  );
}
