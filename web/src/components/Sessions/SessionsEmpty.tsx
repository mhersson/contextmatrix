import type { SessionKind } from '../../types';

interface SessionsEmptyProps {
  kind: SessionKind;
  /** The tab holds sessions but the filters hide them all. */
  filtered: boolean;
  onClear: () => void;
}

export function SessionsEmpty({ kind, filtered, onClear }: SessionsEmptyProps) {
  if (filtered) {
    return (
      <div className="ssn-empty" role="status">
        <span>No sessions match these filters.</span>
        <button type="button" className="ssn-empty-clear" onClick={onClear}>Clear filters</button>
      </div>
    );
  }
  return (
    <div className="ssn-empty" role="status">
      {kind === 'architect' ? 'No blueprints yet.' : 'No chats yet.'}
    </div>
  );
}
