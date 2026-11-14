import type { ChatSession } from '../../types';
import { formatAge, formatExactTime } from '../../lib/format';
import { statusDotColor } from '../../lib/sessions';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { CHAT_DRAG_START_EVENT, CHAT_DRAG_END_EVENT } from '../ChatLayout/dragProtocol';
import { PinIcon } from '../SessionIcons';
import { WaitBadge } from '../WaitBadge';

interface SessionRowProps {
  session: ChatSession;
  onOpen: (id: string) => void;
  onTogglePin: (session: ChatSession) => void;
  waiting?: boolean;
  now: number;
}

export function SessionRow({ session: s, onOpen, onTogglePin, waiting, now }: SessionRowProps) {
  const title = s.title || '(untitled)';
  const pinned = s.pinned === true;
  // Touch has no hover, so the pin stays visible there.
  const touch = isTouchDevice();

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
        className="flex-1 min-w-0 text-left pl-3 pr-1 py-1.5 rounded text-[13.5px] flex items-center gap-2"
        style={{ cursor: touch ? 'pointer' : 'grab' }}
      >
        <span
          className="sb-dot"
          role="img"
          title={s.status}
          aria-label={s.status}
          style={{ backgroundColor: statusDotColor(s.status) }}
        />
        <span className="truncate flex-1">{title}</span>
        {waiting && <WaitBadge />}
        <span className="sb-age" title={formatExactTime(s.last_active)}>
          {formatAge(s.last_active, now)}
        </span>
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
