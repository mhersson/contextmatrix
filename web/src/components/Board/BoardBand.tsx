interface BoardBandProps {
  projectName: string;
  displayName?: string;
  activeAgents: number;
  openCount: number;
  inReviewCount: number;
  shippedToday: number;
  onCreateCard: () => void;
  shippedLast7d?: number;
  shippedPrior7d?: number;
}

/**
 * Header band for the project board. Editorial-engineering style: mono
 * crumb, Fraunces hero title, sub-line with rolling stats, +New Card
 * primary action. Hairline aqua tick fades under the title.
 *
 * Subheader stats count delivery units only (cards where `!parent`);
 * subtasks are excluded so decomposition does not inflate the rolling
 * headline. The caller passes parent-only counts.
 */
export function BoardBand({
  projectName,
  displayName,
  activeAgents,
  openCount,
  inReviewCount,
  shippedToday,
  onCreateCard,
  shippedLast7d,
  shippedPrior7d,
}: BoardBandProps) {
  const title = displayName ?? projectName;

  const showDelta = shippedLast7d !== undefined && shippedPrior7d !== undefined && shippedPrior7d > 0;
  const deltaPct = showDelta
    ? Math.round(((shippedLast7d - shippedPrior7d) / shippedPrior7d) * 100)
    : 0;
  const deltaUp = showDelta && shippedLast7d >= shippedPrior7d;

  return (
    <div className="board-band">
      <div>
        <div className="board-band__crumb">
          <span>Projects</span>
          <span className="dot" />
          <span className="accent">{projectName}</span>
        </div>
        <h2 className="board-band__title">{title}</h2>
        <div className="board-band__sub">
          <span className="board-band__pulse">{activeAgents} agents live</span>
          <span className="board-band__sep">·</span>
          <span>{openCount} open · {inReviewCount} in review · {shippedToday} shipped today</span>
          {shippedLast7d !== undefined && (
            <>
              <span className="board-band__sep">·</span>
              <span>
                {shippedLast7d} shipped this week
                {showDelta && (
                  <>
                    {' '}·{' '}
                    <span style={{ color: deltaUp ? 'var(--green)' : 'var(--red)' }}>
                      {deltaUp ? '+' : ''}
                      {deltaPct}%
                    </span>
                  </>
                )}
              </span>
            </>
          )}
        </div>
      </div>
      <div>
        <button
          type="button"
          onClick={onCreateCard}
          className="px-3 py-2 rounded font-medium bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90 transition-opacity inline-flex items-center gap-2"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2.5} viewBox="0 0 24 24"><path d="M12 5v14M5 12h14" strokeLinecap="round" strokeLinejoin="round" /></svg>
          New Card
        </button>
      </div>
    </div>
  );
}
