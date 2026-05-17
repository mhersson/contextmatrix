interface BoardFooterProps {
  lastSyncLabel: string;
  cardCount: number;
  columnCount: number;
  nowRailOpen?: boolean;
  onToggleNowRail?: () => void;
}

export function BoardFooter({
  lastSyncLabel,
  cardCount,
  columnCount,
  nowRailOpen,
  onToggleNowRail,
}: BoardFooterProps) {
  return (
    <div className="board-footer">
      <span className="board-footer__pulse">{lastSyncLabel}</span>
      <span>{cardCount} cards · {columnCount} columns · 1 board</span>
      {onToggleNowRail ? (
        <button
          type="button"
          onClick={onToggleNowRail}
          className="board-footer__rail-toggle"
          aria-pressed={nowRailOpen}
          title={nowRailOpen ? 'Hide right rail' : 'Show right rail'}
        >
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
            <rect x="3" y="4" width="18" height="16" rx="2" />
            <line x1="15" y1="4" x2="15" y2="20" />
          </svg>
          <span>{nowRailOpen ? 'Hide rail' : 'Show rail'}</span>
        </button>
      ) : (
        <span />
      )}
    </div>
  );
}
