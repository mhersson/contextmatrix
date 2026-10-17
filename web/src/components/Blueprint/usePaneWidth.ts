import { useEffect, useState, type RefObject } from 'react';

/** The element's width from a ResizeObserver; null until the first measurement. */
export function usePaneWidth(ref: RefObject<HTMLElement | null>): number | null {
  const [width, setWidth] = useState<number | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const measured = entries[0]?.contentRect.width;
      if (measured !== undefined) setWidth(Math.round(measured));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return width;
}
