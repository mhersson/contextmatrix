interface SessionsEmptyProps {
  /** Shown while the tab holds nothing at all. */
  emptyText: string;
  /** The tab holds rows but the filters hide them all. */
  filtered: boolean;
  onClear: () => void;
}

export function SessionsEmpty({ emptyText, filtered, onClear }: SessionsEmptyProps) {
  if (filtered) {
    return (
      <div className="ssn-empty" role="status">
        <span>Nothing matches these filters.</span>
        <button type="button" className="ssn-empty-clear" onClick={onClear}>Clear filters</button>
      </div>
    );
  }
  return (
    <div className="ssn-empty" role="status">
      {emptyText}
    </div>
  );
}
