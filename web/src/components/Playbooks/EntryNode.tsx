import type { CSSProperties } from 'react';
import type { PlaybookEntry } from '../../types';

interface EntryNodeProps {
  entry: PlaybookEntry;
  index: number;
}

// Node marker: circled mono sequence number for cards, rotated-square
// (diamond) for manual gates. Completed nodes fill --bg-green with a --green
// check; the agent-active node pulses --aqua (see .pb-pulse in index.css);
// broken refs get a dashed --red border.
export function EntryNode({ entry, index }: EntryNodeProps) {
  const active = entry.card_state === 'in_progress' && !entry.missing;
  const isManual = entry.type === 'manual';
  const style: CSSProperties = {
    width: 22,
    height: 22,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    borderRadius: isManual ? 4 : '50%',
    transform: isManual ? 'rotate(45deg)' : undefined,
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    background: entry.complete ? 'var(--bg-green)' : active ? 'var(--bg-aqua)' : 'var(--bg1)',
    border: `2px ${entry.missing ? 'dashed' : 'solid'} ${
      entry.complete ? 'var(--green)' : active ? 'var(--aqua)' : entry.missing ? 'var(--red)' : 'var(--bg3)'
    }`,
    color: entry.complete ? 'var(--green)' : active ? 'var(--aqua)' : entry.missing ? 'var(--red)' : 'var(--grey1)',
  };
  return (
    <span className={active ? 'pb-pulse' : undefined} style={style} aria-hidden="true">
      <span style={{ transform: isManual ? 'rotate(-45deg)' : undefined }}>
        {entry.complete ? '✓' : index + 1}
      </span>
    </span>
  );
}
