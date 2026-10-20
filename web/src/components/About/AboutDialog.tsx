import { useEffect, useId, useRef, useState } from 'react';
import { useAbout } from '../../hooks/useAbout';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { ChangesTabs } from './ChangesTabs';
import { RunningTable } from './RunningTable';

const muted = { color: 'var(--blueprint-quiet-strong)' };

function Eyebrow({ children }: { children: string }) {
  return (
    <div className="mb-1 text-xs tracking-wider" style={muted}>
      {children}
    </div>
  );
}

function Body({ onClose }: { onClose: () => void }) {
  const { about, seen, refresh, markSeen } = useAbout();
  // Captured once per open so NEW markers survive the markSeen below.
  const [snapshot] = useState(() => seen);
  const [settled, setSettled] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const latest = useRef(about);
  useEffect(() => {
    latest.current = about;
  }, [about]);

  useFocusTrap(dialogRef, true);

  useEffect(() => {
    let live = true;
    void refresh().then((fresh) => {
      const shown = fresh ?? latest.current;
      if (shown) markSeen(shown.components);
      if (live) setSettled(true);
    });
    return () => {
      live = false;
    };
    // Once per open.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // Capture phase and consumed, so page-level Escape handlers (board filter, drawer) do not also fire.
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      e.stopPropagation();
      onClose();
    };
    window.addEventListener('keydown', onKey, { capture: true });
    return () => window.removeEventListener('keydown', onKey, { capture: true });
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 flex w-[40rem] max-w-[calc(100vw-2rem)] flex-col gap-4 rounded-lg border p-5"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        onClick={(e) => e.stopPropagation()}
        // Page shortcuts must not act behind the dialog; the focus trap listens on this element, so it still sees Tab.
        onKeyDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div>
            <h2 id={titleId} className="text-base font-semibold" style={{ color: 'var(--fg)' }}>
              About ContextMatrix
            </h2>
            <p className="text-sm" style={muted}>
              What runs here, and what changed
            </p>
          </div>
          <button type="button" aria-label="Close" onClick={onClose} className="px-1 text-lg leading-none" style={muted}>
            <span aria-hidden="true">×</span>
          </button>
        </div>
        {about ? (
          <>
            <section>
              <Eyebrow>RUNNING</Eyebrow>
              <RunningTable components={about.components} />
            </section>
            <section>
              <Eyebrow>CHANGES</Eyebrow>
              <ChangesTabs components={about.components} snapshot={snapshot} />
            </section>
          </>
        ) : (
          settled && (
            <p className="text-sm" style={{ color: 'var(--fg)' }}>
              Could not load what runs here. Try again later.
            </p>
          )
        )}
      </div>
    </div>
  );
}

export function AboutDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return open ? <Body onClose={onClose} /> : null;
}
