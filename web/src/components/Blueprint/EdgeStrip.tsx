import type { Ref } from 'react';
import { SheetIcon } from './icons';

interface EdgeStripProps {
  label: string;
  hasNew: boolean;
  onOpen: () => void;
  buttonRef?: Ref<HTMLButtonElement>;
}

export function EdgeStrip({ label, hasNew, onOpen, buttonRef }: EdgeStripProps) {
  return (
    <button
      ref={buttonRef}
      type="button"
      className="bp-edge"
      onClick={onOpen}
      aria-label={`Open the ${label} sheet${hasNew ? ', something new' : ''}`}
    >
      <SheetIcon />
      <span className="bp-edge-text">
        <b>{label}</b> sheet
      </span>
      {hasNew && <i className="bp-edge-dot" aria-hidden="true" />}
    </button>
  );
}
