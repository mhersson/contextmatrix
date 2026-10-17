import { useId, useState } from 'react';
import { MAX_MESSAGE_LENGTH } from '../ChatPanel/chatEntryUtils';
import { personaName } from '../Blueprint/stages';
import type { Round } from './parseRound';
import type { RoundMode } from './roundView';

interface RoundChatProps {
  round: Round;
  mode: RoundMode;
  onSendChat?: (text: string) => Promise<void>;
}

/** The dotted separator and the round-wide thread; its box is the session's text input while the round is open. */
export function RoundChat({ round, mode, onSendChat }: RoundChatProps) {
  const [open, setOpen] = useState(round.chat.length > 0);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const chatId = useId();
  const canType = mode === 'open' && onSendChat !== undefined;

  const send = async () => {
    const t = text.trim();
    if (!onSendChat || t === '' || sending) return;
    setSending(true);
    setError(null);
    try {
      await onSendChat(t);
      setText('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send message');
    } finally {
      setSending(false);
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
            {round.chat.map((m, i) => (
              <div key={i} className={m.from === 'user' ? 'me' : 'ai'}>
                <b>{m.from === 'user' ? 'You' : personaName(m.from)}:</b> {m.text}
              </div>
            ))}
            <div className="qb-chat-compose">
              <textarea
                className="bf-input"
                rows={2}
                aria-label="Message about these questions"
                placeholder="Ask about these questions, or type a command (Enter to send)"
                value={text}
                maxLength={MAX_MESSAGE_LENGTH}
                disabled={!canType || sending}
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
