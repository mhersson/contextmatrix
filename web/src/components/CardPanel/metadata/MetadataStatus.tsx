import { useId } from 'react';
import type { Card, ProjectConfig } from '../../../types';
import { runnerStatusStyles } from '../../../types';

interface MetadataStatusProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  runnerAttached: boolean;
  onStateChange: (state: string) => void;
  excludeStateFromPicker?: string | null;
}

/**
 * Status section of the Info rail tab. Renders the state picker, a hint
 * describing the current state's transition options, and the runner-status
 * badge when a runner is attached.
 */
export function MetadataStatus({
  card,
  editedCard,
  config,
  runnerAttached,
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
        disabled={runnerAttached}
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
        {runnerAttached
          ? '🔒 Runner owns this card — only the agent or Stop can transition it.'
          : validTransitions.length === 0
            ? 'No transitions available from this state.'
            : 'Select a target state to move the card through the board.'}
      </div>
      {card.runner_status && runnerStatusStyles[card.runner_status] && (
        <div
          className={`mt-3 px-2 py-1 rounded text-xs inline-block${card.runner_status === 'running' ? ' animate-pulse' : ''}`}
          style={{
            backgroundColor: runnerStatusStyles[card.runner_status].bg,
            color: runnerStatusStyles[card.runner_status].text,
          }}
        >
          {runnerStatusStyles[card.runner_status].label}
        </div>
      )}
    </section>
  );
}
