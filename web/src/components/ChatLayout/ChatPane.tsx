import { useState } from 'react';
import type { Slot, AvailableChat } from './types';
import { SLOTS } from './types';
import { PaneHeader } from './PaneHeader';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { PANE_SOURCE_MIME } from './dragProtocol';

const TOUCH = isTouchDevice();

interface Props {
  slot: Slot;
  chatId: string | null;
  chat?: AvailableChat;
  isFocused: boolean;
  connected?: boolean;
  showSplit: boolean;
  showClose: boolean;
  draggingChatId: string | null;
  onFocus: () => void;
  onClose: () => void;
  onSplit: () => void;
  onDropChat: (chatId: string) => void;
  onMovePane: (fromSlot: Slot) => void;
  onEndSession?: () => void;
  onClearContext?: () => void;
  onReopenChat?: () => void;
  onDeleteChat?: () => void;
  children?: React.ReactNode;
}

export function ChatPane({
  slot,
  chatId,
  chat,
  isFocused,
  connected,
  showSplit,
  showClose,
  draggingChatId,
  onFocus,
  onClose,
  onSplit,
  onDropChat,
  onMovePane,
  onEndSession,
  onClearContext,
  onReopenChat,
  onDeleteChat,
  children,
}: Props) {
  const [isDropTarget, setIsDropTarget] = useState(false);

  const handleDragOver = (e: React.DragEvent) => {
    if (!draggingChatId) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    if (!isDropTarget) setIsDropTarget(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    if (e.currentTarget.contains(e.relatedTarget as Node | null)) return;
    if (isDropTarget) setIsDropTarget(false);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDropTarget(false);
    // Check for pane-source drag first (header drag between panes).
    const fromSlotRaw = e.dataTransfer.getData(PANE_SOURCE_MIME);
    if (fromSlotRaw) {
      // Validate the slot value against the known SLOTS list for forward-compat.
      if (SLOTS.includes(fromSlotRaw as Slot) && fromSlotRaw !== slot) {
        onMovePane(fromSlotRaw as Slot);
      }
      // Same pane or invalid slot: fall through to sidebar path only when
      // the value is not a known slot (unknown MIME value from a future drag
      // source). Same-pane is a no-op.
      if (SLOTS.includes(fromSlotRaw as Slot)) return;
    }
    // Sidebar drag: route to swapPaneChat.
    const dropped = e.dataTransfer.getData('text/plain');
    if (dropped) onDropChat(dropped);
  };

  const isSwap = isDropTarget && draggingChatId && chatId && chatId !== draggingChatId;
  const overlayChatLabel = draggingChatId ?? '';
  const headerDraggable = !TOUCH && chatId != null;

  return (
    <div
      className={`chat-pane${isFocused ? ' chat-pane--focused' : ''}${isDropTarget ? ' chat-pane--drop-target' : ''}`}
      data-slot={slot}
      onClick={onFocus}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {isDropTarget && (
        <div className="chat-pane-drop-overlay" aria-hidden="true">
          <div className="chat-pane-drop-icon">{isSwap ? '⇄' : '↳'}</div>
          <div className="chat-pane-drop-copy">
            {isSwap ? (
              <>Drop to <strong>swap</strong> with this pane</>
            ) : (
              <>Drop to open <strong>{overlayChatLabel}</strong></>
            )}
          </div>
        </div>
      )}
      <PaneHeader
        slot={slot}
        draggable={headerDraggable}
        chatId={chatId}
        chat={chat}
        isFocused={isFocused}
        connected={connected}
        showSplit={showSplit}
        showClose={showClose}
        onClose={onClose}
        onSplit={onSplit}
        onEndSession={onEndSession}
        onClearContext={onClearContext}
        onReopenChat={onReopenChat}
        onDeleteChat={onDeleteChat}
      />
      <div className="chat-pane-body">{children}</div>
    </div>
  );
}
