/** The yellow "waiting for you" signal: a count, or `!` without one. */
export function WaitBadge({ count }: { count?: number }) {
  return <span className="wait-badge">{count ?? '!'}</span>;
}
