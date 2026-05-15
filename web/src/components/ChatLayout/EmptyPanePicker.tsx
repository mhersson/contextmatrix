import { useEffect, useRef, useState } from 'react';
import type { AvailableChat } from './types';

interface Props {
  chats: AvailableChat[]; // pre-filtered: chats not currently in any pane
  onPick: (chatId: string) => void;
  onCancel: () => void;
  onNew: () => void;
}

export function EmptyPanePicker({ chats, onPick, onCancel, onNew }: Props) {
  const [highlighted, setHighlighted] = useState(0);
  const rootRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    rootRef.current?.focus();
  }, []);

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onCancel(); return; }
    if (chats.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHighlighted((i) => Math.min(i + 1, chats.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlighted((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const c = chats[highlighted];
      if (c) onPick(c.id);
    }
  };

  return (
    <div
      ref={rootRef}
      tabIndex={0}
      onKeyDown={handleKey}
      className="chat-pane-empty"
    >
      <div className="chat-pane-empty-title">Pick a chat to open here</div>
      <div className="chat-pane-empty-hint">
        Drag from the sidebar · or press <kbd>esc</kbd> to cancel
      </div>

      {chats.length === 0 ? (
        <div className="chat-pane-empty-none">
          All chats are already open in another pane.
          <button type="button" className="chat-pane-empty-link" onClick={onNew}>+ New chat</button>
        </div>
      ) : (
        <>
          <div className="chat-pane-empty-list">
            {chats.map((c, i) => (
              <button
                key={c.id}
                type="button"
                className={`chat-pane-empty-row${i === highlighted ? ' chat-pane-empty-row--first' : ''}`}
                onClick={() => onPick(c.id)}
                onMouseEnter={() => setHighlighted(i)}
              >
                <span
                  className="chat-pane-empty-dot"
                  style={{
                    backgroundColor:
                      c.status === 'active' || c.status === 'warm-idle'
                        ? 'var(--green)'
                        : 'var(--grey0)',
                  }}
                  aria-hidden="true"
                />
                <span className="chat-pane-empty-name">{c.title}</span>
                {i === highlighted && <span className="chat-pane-empty-key">↵</span>}
              </button>
            ))}
          </div>
          <div className="chat-pane-empty-actions">
            <button type="button" className="chat-pane-empty-btn" onClick={onNew}>+ New chat</button>
            <button
              type="button"
              className="chat-pane-empty-btn chat-pane-empty-btn--ghost"
              onClick={onCancel}
            >Cancel</button>
          </div>
        </>
      )}
    </div>
  );
}
