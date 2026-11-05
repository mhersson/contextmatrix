import { useEffect, useId, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { ByteCount } from './ByteCount';
import { utf8Length } from './chatEntryUtils';

export interface ChatComposerProps {
  onSend: (content: string) => void | Promise<void>;
  sendDisabled: boolean;
  /** Longest message in UTF-8 bytes; over it Send is disabled and nothing is cut. */
  maxBytes: number;
  footer?: React.ReactNode;
  /**
   * Imperative focus trigger: whenever this value changes (and the textarea
   * is mounted), the textarea grabs focus. Leave undefined to opt out.
   */
  focusKey?: string | number;
  /** Bumped after a prompt reply sent from the transcript; focuses the textarea. */
  refocus?: number;
  /** The session whose unsent text this box keeps when it unmounts. Unset keeps none. */
  draftScope?: string;
}

// In memory, not storage: a blueprint and the chats replace each other on
// screen, unmounting their composers, and a reload may lose unsent text as ever.
const drafts = new Map<string, string>();

export function ChatComposer({ onSend, sendDisabled, maxBytes, footer, focusKey, refocus, draftScope }: ChatComposerProps) {
  const [message, setMessage] = useState(() => (draftScope ? drafts.get(draftScope) ?? '' : ''));
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

  useEffect(() => {
    if (!refocus) return;
    textareaRef.current?.focus();
  }, [refocus]);

  const changeMessage = (text: string) => {
    setMessage(text);
    if (!draftScope) return;
    if (text) drafts.set(draftScope, text);
    else drafts.delete(draftScope);
  };

  const bytes = utf8Length(message);
  const isOverLimit = bytes > maxBytes;
  const canSend = message.trim().length > 0 && !sending && !isOverLimit && !sendDisabled;

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.nativeEvent.isComposing && !e.shiftKey) {
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
      changeMessage('');
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
          onChange={(e) => { changeMessage(e.target.value); if (error) setError(null); }}
          onKeyDown={handleKeyDown}
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
        <ByteCount bytes={bytes} max={maxBytes} className="mr-auto" />
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
