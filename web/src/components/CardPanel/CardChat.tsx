import { Suspense, lazy, useCallback, useId, useLayoutEffect, useRef, useState } from 'react';
import type { Card, LogEntry } from '../../types';
import { api, isAPIError } from '../../api/client';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

// Lazy-load the markdown previewer so the chat panel doesn't pay the
// bundle cost until the user opens an HITL session. Reuses the same
// component CardPanelEditor mounts for the description preview.
//
// We deliberately do NOT plumb the theme through here. The chat
// markdown styling is fully driven by CSS custom properties scoped to
// :root and [data-theme="light"] (see .wmde-markdown rules in
// index.css), so dark/light switches automatically without
// data-color-mode. This also keeps ChatEntry test-friendly — it does
// not need a ThemeProvider wrapper.
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

const MAX_MESSAGE_LENGTH = 8000;
const NEAR_BOTTOM_THRESHOLD = 50;

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
 * The conversation log stays visible whenever the parent mounts this
 * component. When HITL is no longer active (runner stopped or card promoted
 * to autonomous) the compose row and Switch-to-Autonomous button are
 * replaced by a thin read-only footer so the transcript is preserved while
 * input is closed.
 */
export function CardChat({ card, cardLogs }: CardChatProps) {
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showText, setShowText] = useState(true);
  const [showToolCalls, setShowToolCalls] = useState(false);
  const [showThinking, setShowThinking] = useState(false);
  const messageId = useId();
  const logContainerRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
  }, []);

  // useLayoutEffect pins the scroll before paint so the new content lands at
  // the bottom on the same frame, matching VirtualLogList.
  useLayoutEffect(() => {
    const el = logContainerRef.current;
    if (!el) return;
    if (userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [cardLogs]);

  const hitlActive = card.runner_status === 'running' && !card.autonomous;

  const filteredLogs = cardLogs.filter((entry) => {
    if (entry.type === 'text') return showText;
    if (entry.type === 'tool_call') return showToolCalls;
    if (entry.type === 'thinking') return showThinking;
    return true;
  });

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
      {/* Filter bar */}
      <div className="flex items-center gap-4 px-4 py-2 border-b border-[var(--bg3)] bg-[var(--bg1)] text-xs font-mono shrink-0">
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--fg)' }}>
          <input
            type="checkbox"
            checked={showText}
            onChange={(e) => setShowText(e.target.checked)}
          />
          Text
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--aqua)' }}>
          <input
            type="checkbox"
            checked={showToolCalls}
            onChange={(e) => setShowToolCalls(e.target.checked)}
          />
          Tool calls
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--grey2)' }}>
          <input
            type="checkbox"
            checked={showThinking}
            onChange={(e) => setShowThinking(e.target.checked)}
          />
          Thinking
        </label>
      </div>

      {/* Log column */}
      <div
        ref={logContainerRef}
        onScroll={handleLogScroll}
        className="flex-1 min-h-[60px] overflow-y-auto overflow-x-hidden px-4 py-4 space-y-3 bg-[var(--bg-dim)]"
        role="log"
        aria-live="polite"
        aria-label="Session chat log"
      >
        {filteredLogs.length === 0 ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          filteredLogs.map((entry, idx) => {
            const prev = idx > 0 ? filteredLogs[idx - 1] : null;
            // Only label the first message in a consecutive run from
            // the same sender — mirrors how messaging apps stack
            // bubbles under a single name header.
            const showSender = !prev || senderFor(prev.type) !== senderFor(entry.type);
            return (
              <ChatEntry
                key={`${entry.ts}-${entry.card_id}-${idx}`}
                entry={entry}
                showSender={showSender}
              />
            );
          })
        )}
      </div>

      {hitlActive ? (
        <>
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

            <button
              type="button"
              onClick={() => setConfirmOpen(true)}
              disabled={promoting}
              className="bf-btn-ghost bf-btn-sm"
              style={{ color: 'var(--orange)', borderColor: 'color-mix(in oklab, var(--orange) 35%, transparent)' }}
            >
              {promoting ? 'Promoting…' : '⇢ Switch to Autonomous'}
            </button>

            {error && (
              <div
                className="text-xs px-2 py-1 rounded font-mono"
                style={{ background: 'var(--bg-red)', color: 'var(--red)' }}
              >
                {error}
              </div>
            )}
          </div>
        </>
      ) : (
        <div
          className="px-4 py-2 text-xs font-mono italic text-center border-t border-[var(--bg3)]"
          style={{ backgroundColor: 'var(--bg4)', color: 'var(--grey2)' }}
          role="status"
        >
          {card.autonomous ? 'Promoted to autonomous — read-only' : 'Session ended — read-only'}
        </div>
      )}

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

function ChatEntry({ entry, showSender }: { entry: LogEntry; showSender: boolean }) {
  // Human-typed messages render as right-aligned bubbles in --bg-blue —
  // the active-agent indicator's blue, deliberately distinct from
  // agent text so the conversation reads like a messaging app. The
  // user is implicitly the sender of right-aligned bubbles, so no
  // sender label is rendered.
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

  // Agent text and orchestrator system announcements render as
  // left-aligned bubbles with markdown — fenced JSON / code blocks in
  // structured status messages and plan summaries get proper code
  // styling instead of raw triple-backticks. The sender label sits
  // above the first bubble in a consecutive run so stacked messages
  // read like a normal messaging-app conversation.
  if (entry.type === 'text' || entry.type === 'system') {
    const isSystem = entry.type === 'system';
    return (
      <div className="flex flex-col items-start gap-0.5">
        {showSender && (
          <span className="text-[10px] uppercase tracking-wide text-[var(--grey1)] pl-1">
            {senderFor(entry.type)}
          </span>
        )}
        <div
          className="max-w-[85%] rounded-lg px-3 py-2 text-sm break-words"
          style={{
            backgroundColor: isSystem ? 'var(--bg-green)' : 'var(--bg2)',
            color: 'var(--fg)',
          }}
        >
          <ChatMarkdown source={entry.content} />
        </div>
      </div>
    );
  }

  // Thinking, tool_call, stderr, gap — keep the document-style accent
  // bar; these are diagnostic streams, not conversation turns.
  return (
    <div
      className="pl-3 border-l-2 text-sm font-mono leading-relaxed whitespace-pre-wrap break-words"
      style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
    >
      {entry.content}
    </div>
  );
}

// senderFor labels left-aligned bubbles. Diagnostic types
// (thinking/tool_call/stderr/gap) and 'user' return "" because they
// either don't render as bubbles or are implicitly the human side; the
// run-grouping logic in the parent uses "" as a distinct group so
// they don't suppress a real sender label on the next text/system
// bubble.
function senderFor(type: LogEntry['type']): string {
  switch (type) {
    case 'text':
      return 'Agent';
    case 'system':
      return 'Orchestrator';
    default:
      return '';
  }
}

// ChatMarkdown renders a chat message body through the same markdown
// previewer the description surface uses. Wrapped in Suspense (the
// previewer is lazy-loaded) with a plain-text fallback so streaming
// frames never flash empty.
function ChatMarkdown({ source }: { source: string }) {
  return (
    <div className="bf-chat-markdown">
      <Suspense
        fallback={
          <div className="whitespace-pre-wrap break-words text-sm">{source}</div>
        }
      >
        <MarkdownPreview source={source} skipHtml />
      </Suspense>
    </div>
  );
}

function accentFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--bg3)';
  }
}

function textFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--fg)';
  }
}
