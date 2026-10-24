import { formatBytes } from './chatEntryUtils';

interface ByteCountProps {
  bytes: number;
  max: number;
  className?: string;
  /** Draws the tone as a tint behind --fg text, for surfaces where yellow or red text misses 4.5:1. */
  tint?: boolean;
}

/** A text's size against its limit, shown from 90 percent of it: yellow, red once over. */
export function ByteCount({ bytes, max, className = '', tint = false }: ByteCountProps) {
  if (bytes <= max * 0.9) return null;
  const tone = bytes > max ? 'red' : 'yellow';
  const style = tint ? { background: `var(--bg-${tone})`, color: 'var(--fg)' } : { color: `var(--${tone})` };
  return (
    <div className={`text-xs font-mono ${tint ? 'w-fit px-1 rounded ' : ''}${className}`} style={style}>
      {formatBytes(bytes)} / {formatBytes(max)}
    </div>
  );
}
