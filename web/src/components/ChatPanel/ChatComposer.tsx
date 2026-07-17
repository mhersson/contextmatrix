import { useEffect, useId, useRef, useState } from 'react';
import { flushSync } from 'react-dom';

const MAX_MESSAGE_LENGTH = 8000;

export interface ChatComposerProps {
  onSend: (content: string) => void | Promise<void>;
  sendDisabled: boolean;
  footer?: React.ReactNode;
  /**
   * Imperative focus trigger: whenever this value changes (and the textarea
   * is mounted), the textarea grabs focus. Leave undefined to opt out.
   */
  focusKey?: string | number;
}

export function ChatComposer({ onSend, sendDisabled, footer, focusKey }: ChatComposerProps) {
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const messageId = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Imperative focus when focusKey changes (multi-pane: pane opened/focused).
  // Skipped during sending; flushSync at send-end re-focuses.
  useEffect(() => {
    if (focusKey === undefined) return;
    if (sendDisabled) return;
    textareaRef.current?.focus();
  }, [focusKey, sendDisabled]);

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
      // only queues the flip - flushSync commits it before the imperative focus
      // so the user can keep typing without re-clicking the textarea.
      flushSync(() => setSending(false));
      textareaRef.current?.focus();
    }
  };

  return (
    <>
      <div className="bf-tk-compose">
        <label htmlFor={messageId} className="sr-only">Message</label>
        <textarea
          ref={textareaRef}
          id={messageId}
          className="bf-input"
          placeholder="Type a message… (Enter to send, Shift+Enter for newline)"
          value={message}
          onChange={(e) => { setMessage(e.target.value); if (error) setError(null); }}
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
  );
}
