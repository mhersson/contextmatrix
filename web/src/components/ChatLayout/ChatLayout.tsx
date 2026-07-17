import { Group, Panel, Separator } from 'react-resizable-panels';
import type { Slot, PaneSlots, PaneSizes, AvailableChat } from './types';
import { SLOTS } from './types';
import { ChatPane } from './ChatPane';
import { EmptyPanePicker } from './EmptyPanePicker';

interface Props {
  panes: PaneSlots;
  focused: Slot | null;
  sizes: PaneSizes;
  availableChats: AvailableChat[];
  draggingChatId: string | null;
  isMobile?: boolean;
  onFocus: (slot: Slot) => void;
  onClose: (slot: Slot) => void;
  onSplit: (slot: Slot) => void;
  onResize: (key: 'col' | 'leftRow' | 'rightRow', sizes: number[]) => void;
  onDropChatOnPane: (slot: Slot, chatId: string) => void;
  onMovePane: (fromSlot: Slot, toSlot: Slot) => void;
  onPickEmptyPane: (slot: Slot, chatId: string) => void;
  onCancelEmpty: (slot: Slot) => void;
  onNewChat: (slot: Slot) => void;
  onEndSession?: (chatId: string) => void;
  onClearContext?: (chatId: string) => void;
  onReopenChat?: (chatId: string) => void;
  onDeleteChat?: (slot: Slot, chatId: string) => void;
  renderPaneBody: (chatId: string, slot: Slot, isFocused: boolean) => React.ReactNode;
}

export function ChatLayout(props: Props) {
  const {
    panes, focused, sizes, availableChats, draggingChatId, isMobile,
    onFocus, onClose, onSplit, onResize, onDropChatOnPane, onMovePane,
    onPickEmptyPane, onCancelEmpty, onNewChat,
    onEndSession, onClearContext, onReopenChat, onDeleteChat, renderPaneBody,
  } = props;

  const occupied = SLOTS.filter((s) => panes[s] != null);
  const count = occupied.length;

  if (count === 0) {
    return <EmptyLayoutHero onNewChat={() => onNewChat('TL')} />;
  }

  // Mobile: render only the focused pane (or fallback to first occupied)
  if (isMobile) {
    const slot = focused && panes[focused] ? focused : (occupied[0] ?? null);
    if (!slot) return <EmptyLayoutHero onNewChat={() => onNewChat('TL')} />;
    return (
      <div className="chat-layout chat-layout--mobile">
        {renderPaneFor(slot, {
          props, occupied, isMobile: true,
        })}
      </div>
    );
  }

  // Desktop: 1, 2, 3, or 4 panes
  const hasLeft = panes.TL != null || panes.BL != null;
  const hasRight = panes.TR != null || panes.BR != null;

  // Single pane (always normalized to TL by useChatLayout)
  if (count === 1) {
    const slot = occupied[0];
    return <div className="chat-layout">{renderPaneFor(slot, { props, occupied, isMobile: false })}</div>;
  }

  // 2-4 panes - has both columns (the hook normalizes so this holds)
  if (!hasLeft || !hasRight) return null;

  const leftHasSplit = panes.TL != null && panes.BL != null;
  const rightHasSplit = panes.TR != null && panes.BR != null;

  return (
    <div className="chat-layout">
      <Group orientation="horizontal">
        <Panel
          defaultSize={sizes.col}
          minSize={20}
          onResize={(size) => onResize('col', [size.asPercentage])}
        >
          {leftHasSplit ? (
            <Group orientation="vertical">
              <Panel
                defaultSize={sizes.leftRow}
                minSize={20}
                onResize={(size) => onResize('leftRow', [size.asPercentage])}
              >
                {renderPaneFor('TL', { props, occupied, isMobile: false })}
              </Panel>
              <Separator className="chat-pane-handle chat-pane-handle--row" />
              <Panel defaultSize={100 - sizes.leftRow} minSize={20}>
                {renderPaneFor('BL', { props, occupied, isMobile: false })}
              </Panel>
            </Group>
          ) : (
            renderPaneFor('TL', { props, occupied, isMobile: false })
          )}
        </Panel>

        <Separator className="chat-pane-handle chat-pane-handle--col" />

        <Panel defaultSize={100 - sizes.col} minSize={20}>
          {rightHasSplit ? (
            <Group orientation="vertical">
              <Panel
                defaultSize={sizes.rightRow}
                minSize={20}
                onResize={(size) => onResize('rightRow', [size.asPercentage])}
              >
                {renderPaneFor('TR', { props, occupied, isMobile: false })}
              </Panel>
              <Separator className="chat-pane-handle chat-pane-handle--row" />
              <Panel defaultSize={100 - sizes.rightRow} minSize={20}>
                {renderPaneFor('BR', { props, occupied, isMobile: false })}
              </Panel>
            </Group>
          ) : (
            renderPaneFor('TR', { props, occupied, isMobile: false })
          )}
        </Panel>
      </Group>
    </div>
  );

  function renderPaneFor(
    slot: Slot,
    ctx: { props: Props; occupied: Slot[]; isMobile: boolean },
  ) {
    const pane = panes[slot];
    if (!pane) return null;

    const chatId = pane.chatId;
    const chat = chatId ? availableChats.find((c) => c.id === chatId) : undefined;
    const isFocused = focused === slot;
    const count = ctx.occupied.length;
    const showSplit = !ctx.isMobile && count < 4 && chatId != null;
    const showClose = !ctx.isMobile;

    const body = chatId
      ? renderPaneBody(chatId, slot, isFocused)
      : (
        <EmptyPanePicker
          chats={availableChats.filter(
            (c) => !SLOTS.some((s) => panes[s]?.chatId === c.id),
          )}
          onPick={(id) => onPickEmptyPane(slot, id)}
          onCancel={() => onCancelEmpty(slot)}
          onNew={() => onNewChat(slot)}
        />
      );

    return (
      <ChatPane
        key={slot}
        slot={slot}
        chatId={chatId}
        chat={chat}
        isFocused={isFocused}
        connected={chat?.status === 'active' || chat?.status === 'warm-idle'}
        showSplit={showSplit}
        showClose={showClose}
        draggingChatId={draggingChatId}
        onFocus={() => onFocus(slot)}
        onClose={() => onClose(slot)}
        onSplit={() => onSplit(slot)}
        onDropChat={(id) => onDropChatOnPane(slot, id)}
        onMovePane={(fromSlot) => onMovePane(fromSlot, slot)}
        onEndSession={chatId && onEndSession ? () => onEndSession(chatId) : undefined}
        onClearContext={chatId && onClearContext ? () => onClearContext(chatId) : undefined}
        onReopenChat={chatId && onReopenChat ? () => onReopenChat(chatId) : undefined}
        onDeleteChat={chatId && onDeleteChat ? () => onDeleteChat(slot, chatId) : undefined}
      >{body}</ChatPane>
    );
  }
}

function EmptyLayoutHero({ onNewChat }: { onNewChat: () => void }) {
  return (
    <div className="chat-layout chat-layout--empty">
      <div className="chat-layout-empty-card">
        <div className="chat-layout-empty-icon" aria-hidden="true">▦</div>
        <div className="chat-layout-empty-title">No chats open</div>
        <div className="chat-layout-empty-hint">
          Pick a chat from the sidebar or
          <button type="button" className="chat-layout-empty-link" onClick={onNewChat}>+ New chat</button>
        </div>
      </div>
    </div>
  );
}
