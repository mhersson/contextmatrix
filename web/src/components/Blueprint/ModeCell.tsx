import { useCallback, useRef, useState } from 'react';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import type { BlueprintView } from '../../types';
import { modeLabel, modeLock } from './mode';
import { ModePopover } from './ModePopover';

export function ModeCell({ chatId, view }: { chatId: string; view: BlueprintView }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const close = useCallback(() => setOpen(false), []);
  const lock = modeLock(view);
  // A republished view can lock the mode under an open popover; the architect would refuse its switch.
  if (lock !== null && open) setOpen(false);
  useMenuDismiss(wrapRef, open, close, triggerRef);
  const label = modeLabel(view.mode);
  const tone = view.mode === 'yolo' ? ' bp-mode--yolo' : '';
  return (
    <div ref={wrapRef} className="bp-mode-wrap">
      {lock !== null ? (
        <span className={`bp-mode${tone}`} title={lock}>
          <span>{label}</span>
          <small>Mode locked</small>
          <span className="sr-only">{lock}</span>
        </span>
      ) : (
        <button
          ref={triggerRef}
          type="button"
          className={`bp-mode${tone}`}
          aria-label={`Mode: ${label}, change mode`}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <span>{label}</span>
          <small>Mode</small>
        </button>
      )}
      {open && lock === null && (
        <ModePopover
          chatId={chatId}
          view={view}
          onClose={() => {
            triggerRef.current?.focus();
            setOpen(false);
          }}
        />
      )}
    </div>
  );
}
