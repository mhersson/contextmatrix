import type { Card, LogEntry } from '../../../types';
import { CardChat } from '../CardChat';

interface ChatTabProps {
  card: Card;
  cardLogs: readonly LogEntry[];
  currentAgentId: string | null;
  onPromptAgentId: () => string | null;
}

/**
 * Chat rail tab — only rendered during an HITL-running session. The
 * wrapping flex container is kept here (not inside CardChat) so the
 * layout concern lives in the tab registry, matching the other tabs.
 */
export function ChatTab({ card, cardLogs, currentAgentId, onPromptAgentId }: ChatTabProps) {
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <CardChat
        card={card}
        cardLogs={cardLogs}
        currentAgentId={currentAgentId}
        onPromptAgentId={onPromptAgentId}
      />
    </div>
  );
}
