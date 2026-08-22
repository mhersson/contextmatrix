interface BoardMicroBandProps {
  projectName: string;
  displayName?: string;
  activeAgents: number;
  openCount: number;
  inReviewCount: number;
  stalled: number;
  shippedToday: number;
  shippedLast7d?: number;
  shippedPrior7d?: number;
  onCreateCard: () => void;
}

/**
 * One-row replacement for BoardBand + MetricsRibbon while the board header is
 * collapsed: small display-font title, the ribbon numbers as an inline mono
 * summary, and a compact New Card action. Counts are parent-only, matching
 * the expanded chrome.
 */
export function BoardMicroBand({
  projectName,
  displayName,
  activeAgents,
  openCount,
  inReviewCount,
  stalled,
  shippedToday,
  shippedLast7d,
  shippedPrior7d,
  onCreateCard,
}: BoardMicroBandProps) {
  const title = displayName ?? projectName;

  const showDelta = shippedLast7d !== undefined && shippedPrior7d !== undefined && shippedPrior7d > 0;
  const deltaPct = showDelta
    ? Math.round(((shippedLast7d - shippedPrior7d) / shippedPrior7d) * 100)
    : 0;
  const deltaUp = showDelta && shippedLast7d >= shippedPrior7d;

  return (
    <div className="board-microband">
      <h2 className="board-microband__title">{title}</h2>
      <div className="board-microband__stats">
        <span className={activeAgents > 0 ? 'board-microband__live' : undefined}>{activeAgents} agents</span>
        <span>{openCount} open</span>
        <span>{inReviewCount} review</span>
        <span className={stalled > 0 ? 'board-microband__alert' : undefined}>{stalled} stalled</span>
        <span>{shippedToday} today</span>
        {shippedLast7d !== undefined && (
          <span>
            {shippedLast7d} · 7d
            {showDelta && (
              <>
                {' '}
                <span style={{ color: deltaUp ? 'var(--green)' : 'var(--red)' }}>
                  {deltaUp ? '+' : ''}
                  {deltaPct}%
                </span>
              </>
            )}
          </span>
        )}
      </div>
      <button
        type="button"
        onClick={onCreateCard}
        className="px-2.5 py-1.5 rounded text-sm font-medium bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90 transition-opacity inline-flex items-center gap-1.5 flex-shrink-0"
      >
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth={2.5} viewBox="0 0 24 24"><path d="M12 5v14M5 12h14" strokeLinecap="round" strokeLinejoin="round" /></svg>
        New Card
      </button>
    </div>
  );
}
