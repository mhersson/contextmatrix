import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { MockupFrame } from './MockupFrame';
import type { MockupSettings } from './frameProtocol';
import { MOCKUP_LABELS } from './labels';
import { viewportWidth } from './viewport';

interface ExpandableMockupProps {
  source: string;
  settings?: MockupSettings;
  title: string;
}

const iconProps = {
  width: 16,
  height: 16,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
} as const;

/** The sheet's mockup frame with a button that opens the same mockup over the whole window. */
export function ExpandableMockup({ source, settings, title }: ExpandableMockupProps) {
  const [full, setFull] = useState(false);
  const expandRef = useRef<HTMLButtonElement>(null);
  // Runs after the focus trap's restore, whose saved element can be a control that took the focus as the overlay opened.
  useEffect(() => {
    if (!full) return;
    const button = expandRef.current;
    return () => button?.focus();
  }, [full]);
  return (
    <div className="mk-expandable">
      <MockupFrame source={source} settings={settings} title={title} />
      <button
        ref={expandRef}
        type="button"
        className="mk-icon-btn mk-expand"
        aria-label={MOCKUP_LABELS.expand}
        onClick={() => setFull(true)}
      >
        <svg {...iconProps}>
          <path d="M15 3h6v6M21 3l-7 7M9 21H3v-6M3 21l7-7" />
        </svg>
      </button>
      {full && <MockupOverlay source={source} settings={settings} title={title} onClose={() => setFull(false)} />}
    </div>
  );
}

function MockupOverlay({ source, settings, title, onClose }: ExpandableMockupProps & { onClose: () => void }) {
  const dialogRef = useRef<HTMLDivElement>(null);
  useFocusTrap(dialogRef, true, dialogRef);
  const declared = viewportWidth(source);
  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label={MOCKUP_LABELS.fullSize}
      tabIndex={-1}
      className="mk-overlay bg-black/50"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      // Escape bubbles through the portal; marking it handled keeps the enclosing sheet from closing beneath the overlay.
      onKeyDown={(e) => {
        if (e.key === 'Escape') e.preventDefault();
      }}
    >
      <button type="button" className="mk-icon-btn mk-overlay-close" aria-label={MOCKUP_LABELS.closeFullSize} onClick={onClose}>
        <svg {...iconProps}>
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
      {/* MockupFrame fills a pane that holds its declared width, so the cap keeps width=390 from laying out wider. */}
      <div className="mk-overlay-pane" style={declared === null ? undefined : { maxWidth: `${declared}px` }}>
        <MockupFrame source={source} settings={settings} title={title} />
      </div>
    </div>,
    document.body,
  );
}
