interface BoardFooterProps {
  lastSyncLabel: string;
  cardCount: number;
  columnCount: number;
}

export function BoardFooter({ lastSyncLabel, cardCount, columnCount }: BoardFooterProps) {
  return (
    <div className="board-footer">
      <span className="board-footer__pulse">{lastSyncLabel}</span>
      <span>{cardCount} cards · {columnCount} columns · 1 board</span>
      <span>⌘K command · esc clear</span>
    </div>
  );
}
