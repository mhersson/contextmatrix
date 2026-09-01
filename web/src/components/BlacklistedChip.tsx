/**
 * Marker for model-catalog entries on the ops.db blacklist (reported
 * incapable by the agent backend). Purely informational: the blacklist keeps
 * such models out of automatic picks, but a card pin deliberately overrides
 * it, so the chip must never disable or block selection.
 */
export function BlacklistedChip() {
  return (
    <span
      className="chip-pill"
      title="Reported incapable; a pin overrides the blacklist"
      style={{
        background: 'color-mix(in oklab, var(--bg-red) 70%, transparent)',
        color: 'var(--red)',
        border: '1px solid color-mix(in oklab, var(--red) 30%, transparent)',
        fontSize: '10px',
        padding: '1px 6px',
        flexShrink: 0,
      }}
    >
      blacklisted
    </span>
  );
}
