import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useChatSessions } from '../../hooks/useChatSessions';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { safeGetString, safeSetString } from '../../utils/safeStorage';
import type { ChatSession, ChatStatus } from '../../types';

const STORAGE_KEY = 'sidebar.chat_section_collapsed';

import { CHAT_DRAG_START_EVENT, CHAT_DRAG_END_EVENT } from '../ChatLayout/dragProtocol';

function statusDotColor(status: ChatStatus): string | null {
  switch (status) {
    case 'active':
      return 'var(--green)';
    case 'warm-idle':
      return 'var(--yellow)';
    default:
      return null;
  }
}

export function ChatSection({ onNewChat }: { onNewChat: () => void }) {
  const { sessions } = useChatSessions();
  const navigate = useNavigate();
  // useMobileSidebar throws when there's no provider. Sidebar is always
  // rendered inside MobileSidebarProvider (App.tsx), so this is safe.
  const mobileSidebar = useMobileSidebar();
  const draggable = !isTouchDevice();

  const [collapsed, setCollapsed] = useState<boolean>(
    () => safeGetString(STORAGE_KEY) === '1',
  );

  const toggle = () => {
    setCollapsed((c) => {
      const next = !c;
      safeSetString(STORAGE_KEY, next ? '1' : '0');

      return next;
    });
  };

  const handleOpen = (id: string) => {
    // ChatPage's deep-link handler opens the chat as a new pane on top of
    // the persisted layout (auto-tile up to 4; LRU evicts the 5th). Then
    // bounces back to /chat so refresh doesn't re-fire the open. This
    // works the same whether the user starts on /chat or any other route.
    navigate(`/chat/${id}`);
    // Close the mobile drawer if we were invoked from inside it.
    mobileSidebar.close();
  };

  const handleNewChat = () => {
    onNewChat();
    mobileSidebar.close();
  };

  return (
    <div className="sb-chat-dock my-1.5 px-2 py-1">
      <div className="flex items-center justify-between px-3 py-1">
        <button
          type="button"
          onClick={toggle}
          className="sb-eyebrow flex items-center gap-1.5"
          aria-expanded={!collapsed}
          aria-controls="chat-section-list"
        >
          <span aria-hidden="true" className="text-[8px]">{collapsed ? '▸' : '▼'}</span>
          Chat
        </button>
        <button
          type="button"
          onClick={handleNewChat}
          className="sb-eyebrow-btn"
          title="New chat"
        >
          + new
        </button>
      </div>
      {!collapsed && (
        <ul id="chat-section-list" className="mt-0.5 space-y-0.5 pb-0.5">
          {sessions.length === 0 ? (
            <li className="px-3 py-2 text-xs italic" style={{ color: 'var(--grey1)' }}>
              No chats yet.
            </li>
          ) : (
            sessions.map((s: ChatSession) => (
              <li key={s.id}>
                <button
                  type="button"
                  onClick={() => handleOpen(s.id)}
                  draggable={draggable}
                  onDragStart={(e) => {
                    e.dataTransfer.setData('text/plain', s.id);
                    e.dataTransfer.effectAllowed = 'move';
                    window.dispatchEvent(
                      new CustomEvent(CHAT_DRAG_START_EVENT, { detail: { chatId: s.id } }),
                    );
                  }}
                  onDragEnd={() => {
                    window.dispatchEvent(new Event(CHAT_DRAG_END_EVENT));
                  }}
                  className="cm-chat-row block w-full text-left px-3 py-1.5 rounded text-[12.5px] flex items-center gap-2"
                  style={{ cursor: draggable ? 'grab' : 'pointer' }}
                >
                  <span className="truncate flex-1">{s.title || '(untitled)'}</span>
                  {statusDotColor(s.status) && (
                    <span
                      className="w-2 h-2 rounded-full shrink-0"
                      style={{ backgroundColor: statusDotColor(s.status)! }}
                    />
                  )}
                </button>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}
