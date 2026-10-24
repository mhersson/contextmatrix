import type { ChatSession } from '../../types';
import { statusDotColor } from '../../lib/sessions';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { CHAT_DRAG_START_EVENT, CHAT_DRAG_END_EVENT } from '../ChatLayout/dragProtocol';
import { BlueprintIcon, PinIcon } from '../SessionIcons';

interface SessionRowProps {
  session: ChatSession;
  onOpen: (id: string) => void;
  onTogglePin: (session: ChatSession) => void;
}

export function SessionRow({ session: s, onOpen, onTogglePin }: SessionRowProps) {
  const title = s.title || '(untitled)';
  const pinned = s.pinned === true;
  // Touch has no hover, so the pin stays visible there.
  const touch = isTouchDevice();
  const dot = statusDotColor(s.status);

  return (
    <li className="cm-chat-row sb-session flex items-center rounded">
      <button
        type="button"
        onClick={() => onOpen(s.id)}
        draggable={!touch}
        onDragStart={(e) => {
          e.dataTransfer.setData('text/plain', s.id);
          e.dataTransfer.effectAllowed = 'move';
          window.dispatchEvent(new CustomEvent(CHAT_DRAG_START_EVENT, { detail: { chatId: s.id } }));
        }}
        onDragEnd={() => {
          window.dispatchEvent(new Event(CHAT_DRAG_END_EVENT));
        }}
        className="flex-1 min-w-0 text-left pl-3 pr-1 py-1.5 rounded text-[12.5px] flex items-center gap-2"
        style={{ cursor: touch ? 'pointer' : 'grab' }}
      >
        {s.kind === 'architect' && (
          <span className="inline-flex shrink-0" style={{ color: 'var(--aqua)' }} title="Blueprint session">
            <BlueprintIcon label="Blueprint session" />
          </span>
        )}
        <span className="truncate flex-1">{title}</span>
        {dot && <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: dot }} />}
      </button>
      <button
        type="button"
        onClick={() => onTogglePin(s)}
        aria-pressed={pinned}
        aria-label={`${pinned ? 'Unpin' : 'Pin'} ${title}`}
        title={pinned ? 'Unpin' : 'Pin'}
        className={`sb-pin${pinned || touch ? ' sb-pin--shown' : ''}`}
      >
        <PinIcon size={12} filled={pinned} />
      </button>
    </li>
  );
}
