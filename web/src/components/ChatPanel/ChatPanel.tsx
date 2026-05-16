import { Suspense, lazy, useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import type { LogEntry } from '../../types';

// Lazy-load the markdown previewer so the chat panel doesn't pay the
// bundle cost until first use. The chat markdown styling is fully driven by
// CSS custom properties, so dark/light switches automatically without
// data-color-mode.
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

const MAX_MESSAGE_LENGTH = 8000;
const NEAR_BOTTOM_THRESHOLD = 50;

export interface ChatPanelProps {
  logs: readonly LogEntry[];
  onSend: (content: string) => void | Promise<void>;
  sendDisabled: boolean;
  /**
   * Optional footer rendered below the compose row. Card-bound chat uses
   * it for the "Switch to Autonomous" button + read-only indicator. Global
   * chat passes nothing.
   */
  footer?: React.ReactNode;
  /**
   * When non-empty, replaces the compose row with a read-only footer
   * showing the message. Used when status is cold/promoted.
   */
  readOnlyMessage?: string;
  /**
   * Imperative-style focus trigger: whenever this value changes (and the
   * compose textarea is mounted, i.e. not in read-only/cold state), the
   * textarea grabs focus. Multi-pane chat passes the active pane's
   * sessionID — so opening / focusing a pane puts the cursor in its
   * compose box without an extra click. Leave undefined to opt out.
   */
  focusKey?: string | number;
}

export function ChatPanel({ logs, onSend, sendDisabled, footer, readOnlyMessage, focusKey }: ChatPanelProps) {
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showText, setShowText] = useState(true);
  const [showToolCalls, setShowToolCalls] = useState(false);
  const [showThinking, setShowThinking] = useState(false);
  const messageId = useId();
  const logContainerRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const userScrolledUpRef = useRef(false);

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
  }, []);

  // useLayoutEffect pins the scroll before paint so new content lands at the
  // bottom on the same frame.
  useLayoutEffect(() => {
    const el = logContainerRef.current;
    if (!el) return;
    if (userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [logs]);

  // Imperative focus when focusKey changes (multi-pane: pane opened/focused).
  // Skipped when the textarea is missing (readOnly/cold) so we don't fight
  // the banner. Also skipped during sending; flushSync at send-end re-focuses.
  useEffect(() => {
    if (focusKey === undefined) return;
    if (readOnlyMessage || sendDisabled) return;
    textareaRef.current?.focus();
  }, [focusKey, readOnlyMessage, sendDisabled]);

  const filteredLogs = logs.filter((e) => {
    if (e.type === 'text') return showText;
    if (e.type === 'tool_call') return showToolCalls;
    if (e.type === 'thinking') return showThinking;
    return true;
  });

  const isOverLimit = message.length > MAX_MESSAGE_LENGTH;
  const canSend = message.trim().length > 0 && !sending && !isOverLimit && !sendDisabled;

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
      await onSend(content);
      setMessage('');
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send message');
    } finally {
      // Browsers drop focus() calls against a disabled input. setSending(false)
      // only queues the flip — flushSync commits it before the imperative focus
      // so the user can keep typing without re-clicking the textarea.
      flushSync(() => setSending(false));
      textareaRef.current?.focus();
    }
  };

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Filter bar */}
      <div className="flex items-center gap-4 px-4 py-2 border-b border-[var(--bg3)] bg-[var(--bg1)] text-xs font-mono shrink-0">
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--fg)' }}>
          <input type="checkbox" checked={showText} onChange={(e) => setShowText(e.target.checked)} />
          Text
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--aqua)' }}>
          <input type="checkbox" checked={showToolCalls} onChange={(e) => setShowToolCalls(e.target.checked)} />
          Tool calls
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--grey2)' }}>
          <input type="checkbox" checked={showThinking} onChange={(e) => setShowThinking(e.target.checked)} />
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
        aria-label="Chat log"
      >
        {filteredLogs.length === 0 ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          filteredLogs.map((entry, idx) => <ChatEntry key={`${entry.ts}-${idx}`} entry={entry} />)
        )}
      </div>

      {readOnlyMessage ? (
        <div
          className="px-4 py-2 text-xs font-mono italic text-center border-t border-[var(--bg3)]"
          style={{ backgroundColor: 'var(--bg4)', color: 'var(--grey2)' }}
          role="status"
        >
          {readOnlyMessage}
        </div>
      ) : (
        <>
          <div className="bf-tk-compose">
            <label htmlFor={messageId} className="sr-only">Message</label>
            <textarea
              ref={textareaRef}
              id={messageId}
              className="bf-input"
              placeholder="Type a message… (Enter to send, Shift+Enter for newline)"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              onKeyDown={handleKeyDown}
              maxLength={MAX_MESSAGE_LENGTH}
              disabled={sending || sendDisabled}
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
            {footer}
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
      )}
    </div>
  );
}

function ChatEntry({ entry }: { entry: LogEntry }) {
  // Structural divider sentinel (kind="divider") rendered as a horizontal
  // rule with a small inline label rather than the normal system message
  // style. The match is on kind (not content) so the rendering survives
  // localised label changes and is unambiguous on REST-bootstrap reload.
  if (entry.kind === 'divider') {
    return (
      <div
        className="flex items-center gap-3 py-2"
        data-testid="chat-divider"
        role="separator"
        aria-label={entry.content || 'divider'}
      >
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
        <span
          className="text-[10px] uppercase tracking-wider font-mono"
          style={{ color: 'var(--grey1)' }}
        >
          {entry.content || 'divider'}
        </span>
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
      </div>
    );
  }

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

  if (entry.type === 'text') {
    return (
      <div className="flex justify-start">
        <div
          className="max-w-[85%] rounded-lg px-3 py-2 text-sm break-words"
          style={{ backgroundColor: 'var(--bg2)', color: 'var(--fg)' }}
        >
          <ChatMarkdown source={entry.content} />
        </div>
      </div>
    );
  }

  if (entry.type === 'system') {
    return (
      <div
        className="pl-3 border-l-2 text-sm leading-relaxed break-words"
        style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
      >
        <ChatMarkdown source={entry.content} />
      </div>
    );
  }

  return (
    <div
      className="pl-3 border-l-2 text-sm text-[var(--fg)] font-mono leading-relaxed whitespace-pre-wrap break-words"
      style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
    >
      {entry.content}
    </div>
  );
}

function ChatMarkdown({ source }: { source: string }) {
  return (
    <div className="bf-chat-markdown">
      <Suspense fallback={<div className="whitespace-pre-wrap break-words text-sm">{source}</div>}>
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
