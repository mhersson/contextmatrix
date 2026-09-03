import type { Card, LogEntry } from '../../../types';
import { CardChat } from '../CardChat';

interface ChatTabProps {
  card: Card;
  cardLogs: readonly LogEntry[];
  instanceId?: string | null;
}

/**
 * Chat rail tab - rendered while chat is live (any running worker session:
 * interactive for HITL, read-only for autonomous) and remains available
 * afterward as long as a transcript exists. The wrapping flex container is
 * kept here (not inside CardChat) so the layout concern lives in the tab
 * registry, matching the other tabs.
 */
export function ChatTab({ card, cardLogs, instanceId }: ChatTabProps) {
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <CardChat card={card} cardLogs={cardLogs} instanceId={instanceId} />
    </div>
  );
}
