interface RunnerConsoleHeaderProps {
  connected: boolean;
  cardFilter: string;
  cardIds: string[];
  onCardFilterChange: (id: string) => void;
  onClear: () => void;
  onClose: () => void;
}

export function RunnerConsoleHeader({
  connected,
  cardFilter,
  cardIds,
  onCardFilterChange,
  onClear,
  onClose,
}: RunnerConsoleHeaderProps) {
  return (
    <div
      className="flex items-center gap-3 px-3 py-1.5 flex-shrink-0 text-xs"
      style={{ borderBottom: '1px solid var(--bg3)' }}
    >
      {/* Title */}
      <span className="flex items-center gap-1.5 font-semibold" style={{ color: 'var(--fg)' }}>
        <span style={{ color: 'var(--aqua)' }} aria-hidden="true">
          &gt;_
        </span>
        Runner Console
      </span>

      {/* Connection status dot */}
      <span
        className="w-2 h-2 rounded-full flex-shrink-0"
        style={{ background: connected ? 'var(--green)' : 'var(--red)' }}
        title={connected ? 'Connected' : 'Disconnected'}
        aria-label={connected ? 'Connected' : 'Disconnected'}
      />

      {/* Spacer */}
      <div className="flex-1" />

      {/* Card ID filter */}
      {cardIds.length > 0 && (
        <select
          value={cardFilter}
          onChange={(e) => onCardFilterChange(e.target.value)}
          className="text-xs rounded px-1.5 py-0.5"
          style={{
            background: 'var(--bg1)',
            color: 'var(--fg)',
            border: '1px solid var(--bg3)',
            fontFamily: 'inherit',
          }}
          aria-label="Filter by card ID"
        >
          <option value="">All cards</option>
          {cardIds.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
      )}

      {/* Clear button */}
      <button
        onClick={onClear}
        className="px-2 py-0.5 rounded text-xs transition-opacity opacity-70 hover:opacity-100"
        style={{ color: 'var(--grey2)', border: '1px solid var(--bg3)' }}
        title="Clear logs"
      >
        Clear
      </button>

      {/* Close button */}
      <button
        onClick={onClose}
        className="flex items-center justify-center w-5 h-5 rounded transition-opacity opacity-70 hover:opacity-100"
        style={{ color: 'var(--grey2)' }}
        title="Close console"
        aria-label="Close console"
      >
        <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path
            fillRule="evenodd"
            d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
            clipRule="evenodd"
          />
        </svg>
      </button>
    </div>
  );
}
