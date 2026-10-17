import { SheetIcon } from './icons';

export function EdgeStrip({ label, hasNew, onOpen }: { label: string; hasNew: boolean; onOpen: () => void }) {
  return (
    <button
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
