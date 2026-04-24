import { useEffect, useId, useRef, useState } from 'react';
import type { Card, LogEntry } from '../../types';
import { api, isAPIError } from '../../api/client';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

const MAX_MESSAGE_LENGTH = 8000;

interface CardChatProps {
  card: Card;
  cardLogs: readonly LogEntry[];
}

/**
 * Two-channel chat panel. Agent output renders as a document column with a
 * left accent bar (handles long plan outputs); human replies render as
 * right-aligned bubbles. Newlines are preserved via `white-space: pre-wrap`.
 * The Send button only lives here — never duplicate it in the panel header.
 *
 * Returns null when the runner isn't running an HITL session — kept identical
 * to the previous gate so parent components don't have to special-case it.
 */
export function CardChat({ card, cardLogs }: CardChatProps) {
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const messageId = useId();
  const logContainerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom on new entries unless the user has scrolled up.
  useEffect(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 80) {
      el.scrollTop = el.scrollHeight;
    }
  }, [cardLogs]);

  if (card.runner_status !== 'running' || card.autonomous) {
    return null;
  }

  const isOverLimit = message.length > MAX_MESSAGE_LENGTH;
  const canSend = message.trim().length > 0 && !sending && !isOverLimit;

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (canSend) void handleSend();
    }
  };

  const handleSend = async () => {
    const content = message.trim();
    if (!content || sending || isOverLimit) return;
    setSending(true);
    try {
      await api.sendCardMessage(card.project, card.id, content);
      setMessage('');
      setError(null);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to send message');
    } finally {
      setSending(false);
    }
  };

  const handlePromoteConfirm = async () => {
    setConfirmOpen(false);
    setPromoting(true);
    setError(null);
    try {
      await api.promoteCardToAutonomous(card.project, card.id);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to promote session');
    } finally {
      setPromoting(false);
    }
  };

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Log column */}
      <div
        ref={logContainerRef}
        className="flex-1 min-h-[60px] overflow-y-auto overflow-x-hidden px-4 py-4 space-y-3 bg-[var(--bg-dim)]"
        role="log"
        aria-live="polite"
        aria-label="Session chat log"
      >
        {cardLogs.length === 0 ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          cardLogs.map((entry, idx) => <ChatEntry key={`${entry.ts}-${entry.card_id}-${idx}`} entry={entry} />)
        )}
      </div>

      {/* Compose */}
      <div className="bf-tk-compose">
        <label htmlFor={messageId} className="sr-only">Message</label>
        <textarea
          id={messageId}
          className="bf-input"
          placeholder="Type a message… (Enter to send, Shift+Enter for newline)"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={handleKeyDown}
          maxLength={MAX_MESSAGE_LENGTH}
          disabled={sending}
          rows={2}
        />
        <button
          type="button"
          onClick={() => void handleSend()}
          disabled={!canSend}
          className="bf-btn-primary"
        >
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>

      <div className="px-[18px] pb-3 flex flex-wrap items-center justify-end gap-2">
        {message.length > MAX_MESSAGE_LENGTH * 0.9 && (
          <div
            className="text-xs font-mono mr-auto"
            style={{ color: isOverLimit ? 'var(--red)' : 'var(--yellow)' }}
          >
            {message.length} / {MAX_MESSAGE_LENGTH}
          </div>
        )}

        {!card.autonomous && (
          <button
            type="button"
            onClick={() => setConfirmOpen(true)}
            disabled={promoting}
            className="bf-btn-ghost bf-btn-sm"
            style={{ color: 'var(--orange)', borderColor: 'color-mix(in oklab, var(--orange) 35%, transparent)' }}
          >
            {promoting ? 'Promoting…' : '⇢ Switch to Autonomous'}
          </button>
        )}

        {error && (
          <div
            className="text-xs px-2 py-1 rounded font-mono"
            style={{ background: 'var(--bg-red)', color: 'var(--red)' }}
          >
            {error}
          </div>
        )}
      </div>

      <ConfirmModal
        open={confirmOpen}
        title="Promote to autonomous?"
        message="The agent will finish the workflow without further input, create a feature branch, and open a PR."
        confirmLabel="Promote"
        cancelLabel="Cancel"
        onConfirm={() => void handlePromoteConfirm()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}

function ChatEntry({ entry }: { entry: LogEntry }) {
  if (entry.type === 'user') {
    return (
      <div className="flex justify-end">
        <div
          className="max-w-[85%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap break-words"
          style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--fg)' }}
        >
          {entry.content}
        </div>
      </div>
    );
  }

  // Document-style agent output with a left accent bar.
  return (
    <div
      className="pl-3 border-l-2 text-sm text-[var(--fg)] font-mono leading-relaxed whitespace-pre-wrap break-words"
      style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
    >
      {entry.content}
    </div>
  );
}

function accentFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--bg3)';
  }
}

function textFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--fg)';
  }
}
