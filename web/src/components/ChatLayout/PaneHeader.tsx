import { useEffect, useRef, useState } from 'react';
import type { AvailableChat, Slot } from './types';
import { PaneAccentStripe } from './PaneAccentStripe';
import { useChatLiveData } from '../../hooks/useChatLiveData';
import { contextPct, modelMaxTokens, useChatModels, usageColor } from '../../utils/chatModels';
import {
  PANE_SOURCE_MIME,
  CHAT_DRAG_START_EVENT,
  CHAT_DRAG_END_EVENT,
} from './dragProtocol';

interface Props {
  slot: Slot;
  draggable: boolean;
  chatId: string | null;
  chat?: AvailableChat;
  isFocused: boolean;
  connected?: boolean;
  showSplit: boolean;
  showClose: boolean;
  onClose: () => void;
  onSplit: () => void;
  onEndSession?: () => void;
  onClearContext?: () => void;
  onReopenChat?: () => void;
  onDeleteChat?: () => void;
}

export function PaneHeader({
  slot,
  draggable,
  chatId,
  chat,
  connected = false,
  showSplit,
  showClose,
  onClose,
  onSplit,
  onEndSession,
  onClearContext,
  onReopenChat,
  onDeleteChat,
}: Props) {
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRootRef = useRef<HTMLDivElement | null>(null);

  // Close the menu on any outside click. Captured at the document level so
  // clicks anywhere outside the pane header (other panes, sidebar, body)
  // dismiss it. Esc also closes.
  useEffect(() => {
    if (!menuOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (!menuRootRef.current?.contains(e.target as Node)) setMenuOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [menuOpen]);

  const title = chatId ? (chat?.title ?? chatId) : 'empty pane';
  const titleStyle = chatId ? undefined : { color: 'var(--grey1)' };
  const status = chat?.status;
  const isRunning = status === 'active' || status === 'warm-idle';
  const isCold = status === 'cold';
  const showMenu = chatId != null && (onEndSession || onClearContext || onReopenChat || onDeleteChat);

  const runAndClose = (fn?: () => void) => () => {
    setMenuOpen(false);
    fn?.();
  };

  const handleDragStart = (e: React.DragEvent<HTMLDivElement>) => {
    if (!chatId) { e.preventDefault(); return; }
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', chatId);
    e.dataTransfer.setData(PANE_SOURCE_MIME, slot);
    window.dispatchEvent(new CustomEvent(CHAT_DRAG_START_EVENT, { detail: { chatId } }));
  };

  const handleDragEnd = () => {
    window.dispatchEvent(new Event(CHAT_DRAG_END_EVENT));
  };

  return (
    <div
      className="chat-pane-header"
      draggable={draggable}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <PaneAccentStripe chatId={chatId} />
      <div className="chat-pane-title">
        <span className="chat-pane-name" style={titleStyle}>{title}</span>
      </div>
      {chatId && (
        <div className="chat-pane-meta">
          <PaneContextUsage chatId={chatId} fallbackModel={chat?.model} />
          <span
            className="chat-pane-status-dot"
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--grey0)' }}
            aria-label={connected ? 'Stream connected' : 'Stream disconnected'}
          />
        </div>
      )}
      <div className="chat-pane-actions">
        {showSplit && chatId && (
          <button
            type="button"
            className="chat-pane-btn"
            draggable={false}
            onDragStart={(e) => e.preventDefault()}
            onClick={(e) => { e.stopPropagation(); onSplit(); }}
            aria-label="Split pane"
            title="Split pane"
          >+</button>
        )}
        {showMenu && (
          <div ref={menuRootRef} className="chat-pane-menu-wrap">
            <button
              type="button"
              className="chat-pane-btn"
              draggable={false}
              onDragStart={(e) => e.preventDefault()}
              onClick={(e) => { e.stopPropagation(); setMenuOpen((o) => !o); }}
              aria-label="More chat actions"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              title="More"
            >⋮</button>
            {menuOpen && (
              <div className="chat-pane-menu" role="menu">
                {isRunning && onEndSession && (
                  <button
                    type="button"
                    className="chat-pane-menu-item"
                    role="menuitem"
                    onClick={(e) => { e.stopPropagation(); runAndClose(onEndSession)(); }}
                  >End session</button>
                )}
                {isRunning && onClearContext && (
                  <button
                    type="button"
                    className="chat-pane-menu-item"
                    role="menuitem"
                    onClick={(e) => { e.stopPropagation(); runAndClose(onClearContext)(); }}
                  >Clear context</button>
                )}
                {isCold && onReopenChat && (
                  <button
                    type="button"
                    className="chat-pane-menu-item"
                    role="menuitem"
                    onClick={(e) => { e.stopPropagation(); runAndClose(onReopenChat)(); }}
                  >Reopen</button>
                )}
                {onDeleteChat && (
                  <>
                    <div className="chat-pane-menu-separator" />
                    <button
                      type="button"
                      className="chat-pane-menu-item chat-pane-menu-item--danger"
                      role="menuitem"
                      onClick={(e) => { e.stopPropagation(); runAndClose(onDeleteChat)(); }}
                    >Delete chat</button>
                  </>
                )}
              </div>
            )}
          </div>
        )}
        {showClose && (
          <button
            type="button"
            className="chat-pane-btn chat-pane-btn--close"
            draggable={false}
            onDragStart={(e) => e.preventDefault()}
            onClick={(e) => { e.stopPropagation(); onClose(); }}
            aria-label="Close pane"
            title="Close pane"
          >×</button>
        )}
      </div>
    </div>
  );
}

/**
 * Compact model + context-usage display for the pane header. The live
 * chat-stream data (context_tokens, model) is published by ChatThread into a
 * module-level store (useChatLiveData) so the PaneHeader — a sibling above
 * ChatThread — can read it without prop-drilling. Falls back to the model
 * id from the persisted session row until the first session_updated event
 * arrives. Hidden entirely when no model is known.
 */
function PaneContextUsage({ chatId, fallbackModel }: { chatId: string; fallbackModel?: string }) {
  const live = useChatLiveData(chatId);
  const models = useChatModels();
  const modelId = live?.model ?? fallbackModel;
  if (!modelId) return null;
  const m = models.find((x) => x.id === modelId);
  const label = m?.label ?? modelId;
  const max = modelMaxTokens(models, modelId);
  const tokens = live?.contextTokens ?? 0;
  const pct = contextPct(tokens, max);

  const tooltip = max > 0
    ? `Context: ${tokens.toLocaleString()} / ${max.toLocaleString()} tokens (${pct}%)`
    : `Context: ${tokens.toLocaleString()} tokens`;

  return (
    <>
      <span className="chat-pane-model-pill" title={label}>{label}</span>
      {max > 0 && tokens > 0 && (
        <span
          className="chat-pane-pct"
          style={{ color: usageColor(pct) }}
          title={tooltip}
        >{pct}%</span>
      )}
    </>
  );
}
