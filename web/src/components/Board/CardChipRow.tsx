import type { Card } from '../../types';
import { gitHubIcon } from '../icons';
import { chipTint, priorityColors, runnerStatusStyles, shortCardId, typeColors } from '../../lib/chip';
import { avatarGradient } from '../../utils/colorHash';

export interface CardChipRowProps {
  card: Card;
  compact?: boolean;
  onParentClick?: (cardId: string) => void;
}

/**
 * Renders the chip row for a board card.
 *
 * compact=true  — collapsed card header: type initial, source badges, parent badge.
 * compact=false — expanded card footer: priority dot, parent, agent, deps, autonomous,
 *                 runner status, branch, labels.
 */
export function CardChipRow({ card, compact = false, onParentClick }: CardChipRowProps) {
  if (compact) {
    return (
      <>
        {card.type !== 'subtask' && (
          <span
            className="chip-pill flex-shrink-0"
            style={chipTint(typeColors[card.type] || 'var(--grey1)')}
            title={card.type}
            aria-label={`Type: ${card.type}`}
          >
            {card.type.charAt(0)}
          </span>
        )}
        {card.source?.system === 'github' && gitHubIcon}
        {card.source && !card.vetted && (
          <span className="chip-pill flex-shrink-0" style={chipTint('var(--yellow)')}>
            unvetted
          </span>
        )}
        {card.parent && (
          <button
            onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
            className="chip-pill flex-shrink-0 hover:opacity-80 transition-opacity"
            style={{ background: 'var(--bg-blue)', color: 'var(--aqua)' }}
            title={`Parent: ${card.parent}`}
            aria-label={`Navigate to parent ${card.parent}`}
          >
            <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
            </svg>
            <span style={{ fontFamily: 'var(--font-mono)' }}>{shortCardId(card.parent)}</span>
          </button>
        )}
      </>
    );
  }

  return (
    <div className="flex items-center flex-wrap gap-2">
      {/* Priority dot */}
      <span
        className="w-2 h-2 rounded-full"
        style={{ backgroundColor: priorityColors[card.priority] || 'var(--grey1)' }}
        title={card.priority}
        aria-label={`Priority: ${card.priority}`}
      />

      {/* Parent ID badge */}
      {card.parent && (
        <button
          onClick={(e) => { e.stopPropagation(); onParentClick?.(card.parent!); }}
          className="chip-pill hover:opacity-80 transition-opacity"
          style={chipTint('var(--aqua)')}
          title={`Parent: ${card.parent}`}
          aria-label={`Navigate to parent ${card.parent}`}
        >
          <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
          </svg>
          <span style={{ fontFamily: 'var(--font-mono)' }}>{shortCardId(card.parent)}</span>
        </button>
      )}

      {/* Agent indicator */}
      {card.assigned_agent && (
        (() => {
          const grad = avatarGradient(card.assigned_agent);
          return (
            <span
              className="chip-pill truncate max-w-[140px] inline-flex items-center gap-1.5 pr-2"
              style={{ backgroundColor: 'color-mix(in srgb, var(--aqua) 16%, transparent)', color: 'var(--aqua)' }}
              title={card.assigned_agent}
            >
              <span
                className="agent-avatar agent-avatar--online"
                style={{ '--av-from': grad.from, '--av-to': grad.to } as React.CSSProperties}
              />
              <span className="truncate">{card.assigned_agent.replace(/^claude-/, '').replace(/^human:/, '')}</span>
            </span>
          );
        })()
      )}

      {/* Dependency status */}
      {card.depends_on && card.depends_on.length > 0 && (
        <span
          className="chip-pill"
          style={chipTint(card.dependencies_met ? 'var(--green)' : 'var(--red)')}
          title={card.dependencies_met ? 'All dependencies met' : 'Blocked by dependencies'}
        >
          {card.dependencies_met ? 'deps met' : 'blocked'}
        </span>
      )}

      {/* Autonomous badge */}
      {card.autonomous && (
        <span
          className="chip-pill"
          style={chipTint('var(--purple)')}
          title="Autonomous mode"
        >
          auto
        </span>
      )}

      {/* Runner status badge */}
      {card.runner_status && runnerStatusStyles[card.runner_status] && (
        <span
          className={`chip-pill${card.runner_status === 'running' ? ' animate-pulse' : ''}`}
          style={{
            backgroundColor: runnerStatusStyles[card.runner_status].bg,
            color: runnerStatusStyles[card.runner_status].text,
          }}
          title={`Runner: ${card.runner_status}`}
          aria-label={`Runner status: ${card.runner_status}`}
        >
          {card.runner_status}
        </span>
      )}

      {/* Branch badge */}
      {card.branch_name && (
        <span
          className="chip-pill truncate max-w-[120px]"
          style={chipTint('var(--green)')}
          title={`Branch: ${card.branch_name}`}
        >
          {card.branch_name.split('/').pop()}
        </span>
      )}

      {/* Labels */}
      {card.labels?.map((label) => (
        <span key={label} className="chip-pill" style={chipTint('var(--purple)')}>
          {label}
        </span>
      ))}
    </div>
  );
}
