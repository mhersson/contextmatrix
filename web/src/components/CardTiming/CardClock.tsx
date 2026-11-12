import { useEffect, useRef, useState } from 'react';
import type { Card } from '../../types';
import { signalIcons } from '../../lib/cardSignals';
import { isWorkedOn, shortDuration, timingSummary } from '../../lib/cardTiming';
import { lastInputWasKeyboard } from '../../lib/inputModality';
import { useNow } from '../../hooks/useNow';
import { HoverPanel } from '../Board/HoverPanel';
import { CardTimingRows } from './CardTimingRows';

const TIMING_BOX_WIDTH = 280;
const TIMING_BOX_INTERVALS = 5;
/** Time to move the pointer from the clock onto its box before it closes. */
const CLOSE_GRACE_MS = 150;
/** The tile shows minutes, so it re-reads the time less often than an open box. */
const TILE_TICK_MS = 15_000;

/**
 * The tile's clock: an icon and the time the card has been in its current
 * state, shown only while an agent holds the card in a state where work
 * happens. Hover, keyboard focus or a tap opens the timing box; Escape, blur,
 * leaving the clock and the box, a press elsewhere, scrolling and resizing
 * close it. The pointer can rest on the box (WCAG 1.4.13).
 */
export function CardClock({ card }: { card: Card }) {
  const visible = isWorkedOn(card);
  const [open, setOpen] = useState(false);
  const now = useNow(visible ? (open ? 1000 : TILE_TICK_MS) : null);
  const anchorRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const id = `card-clock-${card.id}`;

  const closeTimer = useRef<number | undefined>(undefined);
  const cancelClose = () => window.clearTimeout(closeTimer.current);
  const close = () => { cancelClose(); setOpen(false); };
  const show = () => { cancelClose(); setOpen(true); };
  const closeSoon = () => {
    cancelClose();
    closeTimer.current = window.setTimeout(() => setOpen(false), CLOSE_GRACE_MS);
  };
  useEffect(() => () => window.clearTimeout(closeTimer.current), []);
  useEffect(() => {
    if (!open) return;
    const dismiss = () => setOpen(false);
    // A tap elsewhere on a touch screen fires no mouseleave or blur.
    const pressOutside = (e: PointerEvent) => {
      const target = e.target as Node;
      if (anchorRef.current?.contains(target) || panelRef.current?.contains(target)) return;
      setOpen(false);
    };
    window.addEventListener('scroll', dismiss, true);
    window.addEventListener('resize', dismiss);
    document.addEventListener('pointerdown', pressOutside, true);
    return () => {
      window.removeEventListener('scroll', dismiss, true);
      window.removeEventListener('resize', dismiss);
      document.removeEventListener('pointerdown', pressOutside, true);
    };
  }, [open]);

  if (!visible) return null;
  const timing = timingSummary(card, now);
  const time = shortDuration(timing.stateAge);

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        className="card-clock focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)]"
        aria-label={`${timing.state} for ${time}, ${timing.holder} working`}
        aria-describedby={open ? id : undefined}
        onMouseEnter={show}
        onMouseLeave={closeSoon}
        onFocus={() => { if (lastInputWasKeyboard()) show(); }}
        onBlur={close}
        // Opens, never toggles: a hover already opened it, and a tap fires
        // mouseenter first. Leaving, blur, Escape or a press elsewhere close it.
        onClick={(e) => { e.stopPropagation(); show(); }}
        onKeyDown={(e) => {
          if (e.key === 'Escape' && open) {
            e.stopPropagation();
            close();
          }
        }}
      >
        <span className="card-clock__icon" aria-hidden="true">{signalIcons.clock}</span>
        <span>{time}</span>
      </button>
      {open && (
        <HoverPanel
          id={id}
          anchorRef={anchorRef}
          panelRef={panelRef}
          width={TIMING_BOX_WIDTH}
          placement="below"
          onPointerInside={(inside) => (inside ? show() : closeSoon())}
        >
          <div className="card-hover__head">
            <span className="card-hover__id">{card.id}</span>
            <span className="card-timing__title">Timing</span>
          </div>
          <CardTimingRows timing={timing} maxIntervals={TIMING_BOX_INTERVALS} />
        </HoverPanel>
      )}
    </>
  );
}

/** The Info tab's Timing section: the timing box's rows, with every claim. */
export function CardTimingDetails({ card, unclaimedWarningSeconds }: { card: Card; unclaimedWarningSeconds?: number }) {
  const now = useNow(1000);
  return (
    <section className="bf-aside-section">
      <h4>Timing</h4>
      <CardTimingRows timing={timingSummary(card, now, unclaimedWarningSeconds)} />
    </section>
  );
}
