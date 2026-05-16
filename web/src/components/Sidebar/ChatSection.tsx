import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useChatSessions } from '../../hooks/useChatSessions';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { isTouchDevice } from '../../utils/isTouchDevice';
import type { ChatSession, ChatStatus } from '../../types';

const STORAGE_KEY = 'sidebar.chat_section_collapsed';

import { CHAT_DRAG_START_EVENT, CHAT_DRAG_END_EVENT } from '../ChatLayout/dragProtocol';

function statusDotColor(status: ChatStatus): string | null {
  switch (status) {
    case 'active':
      return 'var(--aqua)';
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

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem(STORAGE_KEY) === '1';
    } catch {
      return false;
    }
  });

  const toggle = () => {
    setCollapsed((c) => {
      const next = !c;
      try {
        localStorage.setItem(STORAGE_KEY, next ? '1' : '0');
      } catch {
        /* ignore */
      }
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
    <div
      className="px-2 py-2"
      style={
        !collapsed
          ? { backgroundColor: 'color-mix(in oklab, var(--bg-dim) 90%, black)' }
          : undefined
      }
    >
      <div className="flex items-center justify-between px-3 py-1">
        <button
          type="button"
          onClick={toggle}
          className="text-sm font-medium flex items-center gap-1"
          style={{ color: 'var(--fg)' }}
          aria-expanded={!collapsed}
          aria-controls="chat-section-list"
        >
          <span aria-hidden="true">{collapsed ? '▸' : '▼'}</span>
          Chat
        </button>
        <button
          type="button"
          onClick={handleNewChat}
          className="text-xs px-2 py-0.5 rounded hover:opacity-80"
          style={{ color: 'var(--green)', backgroundColor: 'var(--bg1)' }}
          title="New chat"
        >
          + new
        </button>
      </div>
      {!collapsed && (
        <ul id="chat-section-list" className="mt-1 space-y-0.5">
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
                  className="block w-full text-left px-3 py-1.5 rounded text-sm flex items-center gap-2"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--grey2)',
                    cursor: draggable ? 'grab' : 'pointer',
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.backgroundColor = 'var(--bg1)';
                    e.currentTarget.style.color = 'var(--fg)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.backgroundColor = 'transparent';
                    e.currentTarget.style.color = 'var(--grey2)';
                  }}
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
