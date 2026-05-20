// Module-level formatter — re-used across every render, never re-created.
// Locale pinned to 'en-GB' to guarantee HH:MM (24-hour) output regardless of
// the runtime's default locale.
const hhmmFormatter = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

/** Returns an HH:MM string for the given ISO timestamp, or null on bad input. */
// eslint-disable-next-line react-refresh/only-export-components
export function formatHHMM(ts: string): string | null {
  if (!ts) return null;
  const ms = Date.parse(ts);
  if (Number.isNaN(ms)) return null;
  return hhmmFormatter.format(new Date(ms));
}

/** Returns a locale-formatted date+time string for use as a tooltip, or null on bad input.
 *  Pinned to 'en-GB' for test determinism. */
// eslint-disable-next-line react-refresh/only-export-components
export function formatTitle(ts: string): string | null {
  if (!ts) return null;
  const ms = Date.parse(ts);
  if (Number.isNaN(ms)) return null;
  return new Date(ms).toLocaleString('en-GB');
}

/** Pure presentational timestamp label rendered above chat bubbles. */
export function TimestampLabel({ hhmm, title, dateTime }: { hhmm: string; title: string; dateTime: string }) {
  return (
    <time
      dateTime={dateTime}
      title={title}
      className="text-[10px] font-mono mb-1"
      style={{ color: 'var(--grey1)' }}
    >
      {hhmm}
    </time>
  );
}
