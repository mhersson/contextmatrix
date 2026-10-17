import type { StampText } from './sheetModel';

export function Stamp({ stamp }: { stamp: StampText }) {
  return (
    <div
      className={`bp-stamp bp-stamp--${stamp.tone}`}
      role="note"
      aria-label={`${stamp.title} ${stamp.detail}`.trim()}
    >
      {stamp.title}
      {stamp.detail && <small>{stamp.detail}</small>}
    </div>
  );
}
