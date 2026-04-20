import { useEffect, useRef, useState } from 'react';
import type { Card, LogEntry } from '../../types';
import { api, isAPIError } from '../../api/client';
import { LogLine } from '../RunnerConsole/LogLine';

const MAX_MESSAGE_LENGTH = 8000;
const NEAR_BOTTOM_THRESHOLD = 50;

interface CardChatProps {
  card: Card;
  cardLogs: readonly LogEntry[];
}

export function CardChat({ card, cardLogs }: CardChatProps) {
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const logContainerRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);
  // Throttle scroll measurements to once per animation frame to avoid doing
  // layout work on every native scroll event when the log list is long.
  const rafIdRef = useRef<number | null>(null);

  // Auto-scroll to bottom unless user has scrolled up
  const handleScroll = () => {
    if (rafIdRef.current !== null) return;
    rafIdRef.current = requestAnimationFrame(() => {
      rafIdRef.current = null;
      const el = logContainerRef.current;
      if (!el) return;
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
    });
  };

  useEffect(() => {
    return () => {
      if (rafIdRef.current !== null) cancelAnimationFrame(rafIdRef.current);
    };
  }, []);

  useEffect(() => {
    const el = logContainerRef.current;
    if (!el || userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [cardLogs]);

  if (card.runner_status !== 'running' || card.autonomous) {
    return null;
  }

  const isOverLimit = message.length > MAX_MESSAGE_LENGTH;
  const canSend = message.trim().length > 0 && !sending && !isOverLimit;

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (canSend) {
        void handleSend();
      }
    }
  };

  const handleSend = async () => {
    const content = message.trim();
    if (!content || sending || isOverLimit) return;
    setSending(true);
    setError(null);
    try {
      await api.sendCardMessage(card.project, card.id, content);
      setMessage('');
    } catch (err) {
      const msg = isAPIError(err) ? err.error : 'Failed to send message';
      setError(msg);
    } finally {
      setSending(false);
    }
  };

  const handlePromote = async () => {
    const confirmed = window.confirm(
      'Promote this session to autonomous? The agent will finish the workflow without further input, create a feature branch, and open a PR.',
    );
    if (!confirmed) return;
    setPromoting(true);
    setError(null);
    try {
      // Promote is idempotent server-side: an already-autonomous card returns
      // the current card with 202 rather than an error. No client-side
      // branch on ALREADY_AUTONOMOUS is needed.
      await api.promoteCardToAutonomous(card.project, card.id);
    } catch (err) {
      const msg = isAPIError(err) ? err.error : 'Failed to promote session';
      setError(msg);
    } finally {
      setPromoting(false);
    }
  };

  return (
    <div className="flex flex-col h-full gap-2">
      <label className="block text-xs text-[var(--grey1)]">Session Chat</label>

      {/* Log list — fills remaining height when chat panel is active */}
      <div
        ref={logContainerRef}
        className="rounded bg-[var(--bg-dim)] border border-[var(--bg3)] overflow-y-auto flex-1 min-h-[60px] font-mono"
        onScroll={handleScroll}
      >
        {cardLogs.length === 0 ? (
          <div className="flex items-center justify-center h-[60px] text-xs" style={{ color: 'var(--grey1)' }}>
            No messages yet
          </div>
        ) : (
          cardLogs.map((entry, idx) => (
            <LogLine key={`${entry.ts}-${entry.card_id}-${idx}`} entry={entry} />
          ))
        )}
      </div>

      {/* Input row */}
      <div className="flex gap-2 items-end">
        <textarea
          className="flex-1 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] px-3 py-2 resize-none focus:outline-none focus:border-[var(--aqua)] placeholder-[var(--grey0)] disabled:opacity-50"
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
          className="px-3 py-2 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-90 transition-opacity text-sm disabled:opacity-40 whitespace-nowrap"
        >
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>

      {/* Character count warning */}
      {message.length > MAX_MESSAGE_LENGTH * 0.9 && (
        <div className="text-xs text-right" style={{ color: isOverLimit ? 'var(--red)' : 'var(--yellow)' }}>
          {message.length} / {MAX_MESSAGE_LENGTH}
        </div>
      )}

      {/* Switch to Autonomous row */}
      {!card.autonomous && (
        <div className="flex justify-end">
          <button
            type="button"
            onClick={() => void handlePromote()}
            disabled={promoting}
            className="px-3 py-1.5 rounded text-sm hover:opacity-90 transition-opacity disabled:opacity-50"
            style={{ background: 'var(--bg-yellow)', color: 'var(--orange)' }}
          >
            {promoting ? 'Promoting…' : 'Switch to Autonomous'}
          </button>
        </div>
      )}

      {/* Inline error */}
      {error && (
        <div className="text-xs px-2 py-1 rounded" style={{ background: 'var(--bg-red)', color: 'var(--red)' }}>
          {error}
        </div>
      )}
    </div>
  );
}
