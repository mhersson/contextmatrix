import type { Card } from '../../types';
import { formatRelativeTime } from './utils';

interface CardPanelAgentProps {
  card: Card;
  canClaim: boolean;
  canRelease: boolean;
  onClaim: () => void;
  onRelease: () => void;
}

export function CardPanelAgent({
  card,
  canClaim,
  canRelease,
  onClaim,
  onRelease,
}: CardPanelAgentProps) {
  return (
    <div className="p-3 rounded bg-[var(--bg0)] border border-[var(--bg3)]">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-xs text-[var(--grey1)] mb-1">Assigned Agent</div>
          {card.assigned_agent ? (
            <div className="flex items-center gap-2">
              <span className="text-sm text-[var(--aqua)]">{card.assigned_agent}</span>
              {card.last_heartbeat && (
                <span className="text-xs text-[var(--grey0)]">
                  · {formatRelativeTime(card.last_heartbeat)}
                </span>
              )}
            </div>
          ) : (
            <span className="text-sm text-[var(--grey0)]">Unassigned</span>
          )}
        </div>
        <div>
          {canClaim && (
            <button
              onClick={onClaim}
              className="px-3 py-1.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-90 transition-opacity text-sm"
            >
              Claim
            </button>
          )}
          {canRelease && (
            <button
              onClick={onRelease}
              className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm"
            >
              Release
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
