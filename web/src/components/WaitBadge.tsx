/** The yellow "waiting for you" signal: a count, or `!` without one. */
export function WaitBadge({ count }: { count?: number }) {
  return (
    <span className="wait-badge">
      {count ?? (
        <>
          <span aria-hidden="true">!</span>
          <span className="sr-only">, waiting for you</span>
        </>
      )}
    </span>
  );
}
