import { useId, useLayoutEffect, useRef, useState } from 'react';
import { MAX_MESSAGE_LENGTH } from '../ChatPanel/chatEntryUtils';
import type { Round, RoundChatMessage } from './parseRound';
import type { RoundMode } from './roundView';
import { RoundThread } from './RoundThread';

interface RoundChatProps {
  round: Round;
  mode: RoundMode;
  /** Messages sent from the box since this frame, shown after its thread until the architect republishes the round. */
  sent?: readonly string[];
  onSendChat?: (text: string) => Promise<void>;
}

/** The dotted separator and the round-wide thread; its box is the session's text input while the round is open. */
export function RoundChat({ round, mode, sent = [], onSendChat }: RoundChatProps) {
  const thread: RoundChatMessage[] = [...round.chat, ...sent.map((text) => ({ from: 'user', text }))];
  const [open, setOpen] = useState(thread.length > 0);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const chatId = useId();
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const threadRef = useRef<HTMLDivElement>(null);
  // The box is the only text input while a round is open, so it stays enabled; this ref blocks a double send.
  const inFlight = useRef(false);
  const canType = mode === 'open' && onSendChat !== undefined;

  useLayoutEffect(() => {
    const el = threadRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [thread.length, open]);

  const send = async () => {
    const t = text.trim();
    if (!onSendChat || t === '' || inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setError(null);
    try {
      await onSendChat(t);
      setText('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send message');
    } finally {
      inFlight.current = false;
      setSending(false);
      inputRef.current?.focus();
    }
  };

  return (
    <>
      <div className="qb-sep" />
      <div className="qb-global">
        <div className="qb-opt">
          <button
            type="button"
            className="qb-opt-top"
            aria-expanded={open}
            aria-controls={open ? chatId : undefined}
            onClick={() => setOpen((o) => !o)}
          >
            <span className="qb-n" aria-hidden="true">
              •
            </span>
            <span className="qb-title">Chat about these questions</span>
            <span className="qb-sum">
              Ask about any of the questions or how they fit together; the architect may research more and update the options.
            </span>
          </button>
        </div>
        {open && (
          <div className="qb-chat" id={chatId}>
            {thread.length > 0 && (
              <div ref={threadRef} className="qb-thread" role="log" aria-label="Chat thread">
                <RoundThread chat={thread} />
              </div>
            )}
            <div className="qb-chat-compose">
              <textarea
                ref={inputRef}
                className="bf-input"
                rows={2}
                aria-label="Message about these questions"
                placeholder="Ask about these questions, or type a command (Enter to send)"
                value={text}
                maxLength={MAX_MESSAGE_LENGTH}
                disabled={!canType}
                onChange={(e) => {
                  setText(e.target.value);
                  if (error) setError(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    void send();
                  }
                }}
              />
              <button type="button" className="bf-btn-primary" disabled={!canType || sending || text.trim() === ''} onClick={() => void send()}>
                {sending ? 'Sending…' : 'Send'}
              </button>
            </div>
            {error && (
              <div role="alert" className="qb-error">
                {error}
              </div>
            )}
          </div>
        )}
      </div>
    </>
  );
}
