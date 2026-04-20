import type { ReactNode } from 'react';
import type { Card, LogEntry } from '../../types';
import { CardChat } from './CardChat';

interface CardPanelBodyProps {
  card: Card;
  cardLogs: readonly LogEntry[];
  isHITLRunning: boolean;
  children: ReactNode;
}

/**
 * Renders the scrollable body of the CardPanel. Picks between two layouts:
 *
 *  - Split layout (HITL running: runner_status === 'running' AND NOT autonomous):
 *    a top scroll region (capped at 50% of the panel height) plus a chat region
 *    that fills the remaining space. `data-testid`s: `body-split`,
 *    `body-top-section`, `body-chat-region`.
 *
 *  - Single-scroll layout (default): one scroll container holding all children.
 *    `data-testid`: `body-single`.
 *
 * See `web/CLAUDE.md` ("CardPanel active-session layout") for the full contract
 * this component upholds.
 */
export function CardPanelBody({
  card,
  cardLogs,
  isHITLRunning,
  children,
}: CardPanelBodyProps) {
  if (isHITLRunning) {
    return (
      <div className="flex flex-col flex-1 min-h-0" data-testid="body-split">
        {/* Top scroll region — capped so chat always gets at least half the panel */}
        <div
          className="overflow-y-auto overflow-x-hidden p-4 space-y-4 max-h-[50%] min-h-0"
          data-testid="body-top-section"
        >
          {children}
        </div>

        {/* Bottom chat region — fills remaining height */}
        <div
          className="flex-1 min-h-0 flex flex-col p-4 pt-0"
          data-testid="body-chat-region"
        >
          <CardChat card={card} cardLogs={cardLogs} />
        </div>
      </div>
    );
  }

  return (
    <div
      className="p-4 space-y-4 overflow-y-auto overflow-x-hidden flex-1 min-h-0"
      data-testid="body-single"
    >
      {children}
    </div>
  );
}
