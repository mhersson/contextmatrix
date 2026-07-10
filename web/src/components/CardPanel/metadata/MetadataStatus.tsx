import { useId } from 'react';
import type { Card, ProjectConfig } from '../../../types';
import { workerStatusStyles } from '../../../lib/chip';

interface MetadataStatusProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  workerAttached: boolean;
  onStateChange: (state: string) => void;
  excludeStateFromPicker?: string | null;
}

/**
 * Status section of the Info rail tab. Renders the state picker, a hint
 * describing the current state's transition options, and the worker-status
 * badge when a worker is attached.
 */
export function MetadataStatus({
  card,
  editedCard,
  config,
  workerAttached,
  onStateChange,
  excludeStateFromPicker,
}: MetadataStatusProps) {
  const stateId = useId();
  const validTransitions = (config.transitions[card.state] || []).filter(
    (s) => s !== excludeStateFromPicker,
  );

  return (
    <section className="bf-aside-section">
      <h4>Status</h4>
      <select
        id={stateId}
        value={editedCard.state}
        disabled={workerAttached}
        onChange={(e) => onStateChange(e.target.value)}
        className="bf-state-select"
        aria-label="State"
      >
        <option value={card.state}>{card.state.replace(/_/g, ' ')} (current)</option>
        {validTransitions.map((s) => (
          <option key={s} value={s}>
            → {s.replace(/_/g, ' ')}
          </option>
        ))}
      </select>
      <div className="font-mono mt-2" style={{ fontSize: '11px', color: 'var(--grey1)', lineHeight: 1.45 }}>
        {workerAttached
          ? '🔒 Worker owns this card — only the agent or Stop can transition it.'
          : validTransitions.length === 0
            ? 'No transitions available from this state.'
            : 'Select a target state to move the card through the board.'}
      </div>
      {card.worker_status && workerStatusStyles[card.worker_status] && (
        <div
          className={`mt-3 px-2 py-1 rounded text-xs inline-block${card.worker_status === 'running' ? ' animate-pulse' : ''}`}
          style={{
            backgroundColor: workerStatusStyles[card.worker_status].bg,
            color: workerStatusStyles[card.worker_status].text,
          }}
        >
          {workerStatusStyles[card.worker_status].label}
        </div>
      )}
    </section>
  );
}
