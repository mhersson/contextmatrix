import { useRef, useState } from 'react';
import type { AvailableChat, Slot } from './types';
import { PaneAccentStripe } from './PaneAccentStripe';
import { PLAIN_LANGUAGE, UNATTENDED } from '../../pages/Chat/blueprintForm';
import { useChatLiveData } from '../../hooks/useChatLiveData';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import { toggleSheet, useSheetButton } from '../Blueprint/sheetButtonStore';
import { contextPct, formatCostTooltip, modelMaxTokens, useChatModels, usageColor } from '../../utils/chatModels';
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
  const sheetButton = useSheetButton(chatId);

  useMenuDismiss(menuRootRef, menuOpen, () => setMenuOpen(false));

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
          {chat?.kind === 'architect' && <BlueprintMeta chatId={chatId} mode={chat.mode} capUsd={chat.cost_cap_usd} />}
          {chat?.kind === 'architect' && <PlainLanguageBadge chatId={chatId} fromRow={chat.plain_language === true} />}
          {chat?.kind === 'architect' && <UnattendedBadge chatId={chatId} fromRow={chat.unattended === true} />}
          <PaneContextUsage chatId={chatId} fallbackModel={chat?.model} blueprint={chat?.kind === 'architect'} />
          <span
            className="chat-pane-status-dot"
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--grey0)' }}
            aria-label={connected ? 'Stream connected' : 'Stream disconnected'}
          />
        </div>
      )}
      <div className="chat-pane-actions">
        {chatId && sheetButton && (
          <button
            type="button"
            className={`chat-pane-btn chat-pane-sheet-btn${sheetButton.open ? ' chat-pane-sheet-btn--on' : ''}`}
            draggable={false}
            onDragStart={(e) => e.preventDefault()}
            onClick={(e) => {
              e.stopPropagation();
              toggleSheet(chatId);
            }}
            aria-pressed={sheetButton.open}
            aria-label={sheetButton.hasNew && !sheetButton.open ? 'Sheet, something new' : 'Sheet'}
          >
            Sheet
            {sheetButton.hasNew && !sheetButton.open && <i className="chat-pane-sheet-dot" aria-hidden="true" />}
          </button>
        )}
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
                {isRunning && onClearContext && chat?.kind !== 'architect' && (
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
 * module-level store (useChatLiveData) so the PaneHeader - a sibling above
 * ChatThread - can read it without prop-drilling. Falls back to the model
 * id from the persisted session row until the first session_updated event
 * arrives. Hidden entirely when no model is known. The context window
 * denominator always comes from `GET /api/chats/models` (`useChatModels`),
 * in every source mode - config, endpoint, and openrouter alike. A blueprint
 * session shows the architect's spend from its view instead of the estimate.
 */
function PaneContextUsage({ chatId, fallbackModel, blueprint }: { chatId: string; fallbackModel?: string; blueprint: boolean }) {
  const live = useChatLiveData(chatId);
  const { models } = useChatModels();
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

  // A blueprint shows what the architect recorded, so nothing until its view loads; never CM's token estimate.
  const spent = live?.blueprintSpentUsd;
  const cost = spent ?? (blueprint ? undefined : live?.estimatedCostUsd);
  const showCost = cost != null && cost > 0;
  const costTooltip = showCost
    ? formatCostTooltip({
        promptTokens: live?.promptTokens,
        completionTokens: live?.completionTokens,
        cacheReadTokens: live?.cacheReadTokens,
        cacheCreationTokens: live?.cacheCreationTokens,
      })
    : '';

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
      {showCost && (
        <span
          className="chat-pane-cost"
          style={{ color: 'var(--grey1)' }}
          aria-label={spent !== undefined ? `Spent ${cost.toFixed(2)} dollars` : `Estimated cost ${cost.toFixed(2)} dollars`}
          title={costTooltip || undefined}
        >${cost.toFixed(2)}</span>
      )}
    </>
  );
}

/** Marks a plain-language blueprint. The view's flag wins once it loads, so an
 *  architect that predates the flag shows no badge. */
function PlainLanguageBadge({ chatId, fromRow }: { chatId: string; fromRow: boolean }) {
  const plain = useChatLiveData(chatId)?.blueprintPlainLanguage ?? fromRow;
  if (!plain) return null;
  return <span className="chat-pane-model-pill chat-pane-plain-pill">{PLAIN_LANGUAGE.label}</span>;
}

/** Marks an unattended blueprint. The view's flag wins once it loads. */
function UnattendedBadge({ chatId, fromRow }: { chatId: string; fromRow: boolean }) {
  const unattended = useChatLiveData(chatId)?.blueprintUnattended ?? fromRow;
  if (!unattended) return null;
  return <span className="chat-pane-model-pill chat-pane-plain-pill">{UNATTENDED.label}</span>;
}

/** A blueprint session's kind, mode and cost cap. The mode is the view's once
 *  it loads, since the row can hold a switch the architect refused. A zero cap
 *  means the architect's own default applies, so no amount is shown. */
function BlueprintMeta({ chatId, mode, capUsd }: { chatId: string; mode?: string; capUsd?: number }) {
  const live = useChatLiveData(chatId);
  const cap = capUsd != null && capUsd > 0 ? `$${capUsd.toFixed(2)} cap` : undefined;
  const text = ['Blueprint', live?.blueprintMode ?? mode, cap].filter(Boolean).join(' · ');
  return (
    <span className="chat-pane-model-pill chat-pane-blueprint-pill" title={text}>
      {text}
    </span>
  );
}
