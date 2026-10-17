import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';

const AT_END_PX = 48;

/** Keeps a scroller at its end while `content` grows, unless the reader scrolled up from the end. */
export function useFollowEnd(ref: RefObject<HTMLElement | null>, content: string | undefined): void {
  const atEndRef = useRef(true);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onScroll = () => {
      atEndRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < AT_END_PX;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, [ref]);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && content !== undefined && atEndRef.current) el.scrollTop = el.scrollHeight;
  }, [ref, content]);
}
