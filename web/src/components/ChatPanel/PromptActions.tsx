import { useEffect, useId, useRef, useState } from 'react';
import { MAX_MESSAGE_LENGTH } from './chatEntryUtils';
import type { ChatPrompt, PromptAction, PromptState } from './prompts';

interface PromptActionsProps {
  prompt: ChatPrompt;
  status: PromptState['status'];
  /** The answering action's label; null when a free-text reply answered. */
  answer?: string | null;
  /** Sends a reply as the user's message; set only on the open prompt. */
  onReply?: (text: string) => Promise<void> | void;
  /** Draws a recommended action as the primary button, with no visible chip. */
  recommendedPrimary?: boolean;
}

/**
 * Reply buttons under an architect prompt, or a one-line summary once it is
 * closed. A click sends the action's reply through the normal send path; the
 * prompt closes when that user row reaches the transcript.
 */
export function PromptActions({ prompt, status, answer, onReply, recommendedPrimary }: PromptActionsProps) {
  const [phase, setPhase] = useState<'idle' | 'sending' | 'sent'>('idle');
  const [error, setError] = useState<string | null>(null);
  const [feedbackFor, setFeedbackFor] = useState<PromptAction | null>(null);
  const [feedback, setFeedback] = useState('');
  const feedbackId = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const returnFocusRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (feedbackFor) textareaRef.current?.focus();
  }, [feedbackFor]);

  // Disabling the controls during a send drops focus; a failed send hands it back.
  useEffect(() => {
    if (phase !== 'idle' || !error) return;
    (textareaRef.current ?? returnFocusRef.current)?.focus();
  }, [phase, error]);

  if (status === 'answered' || status === 'superseded') {
    const summary =
      status === 'superseded' ? 'No longer open' : answer ? `Answered: ${answer}` : 'Answered below';
    const offered = prompt.kind !== 'gate' && prompt.actions.length > 0;
    return (
      <div className="mt-2">
        {offered && (
          <ul
            data-testid="prompt-options"
            aria-label="Options offered"
            className="chat-prompt-muted m-0 mb-1 flex list-none flex-col gap-1 p-0 text-xs"
          >
            {prompt.actions.map((a, i) => {
              const chosen = status === 'answered' && !!answer && a.label === answer;
              return (
                <li
                  key={`${i}:${a.label}`}
                  data-chosen={chosen ? 'true' : undefined}
                  className="border-l-2 pl-2"
                  style={{ borderColor: chosen ? 'var(--prompt-question-accent)' : 'transparent' }}
                >
                  {chosen && <span aria-hidden="true">{'\u2713 '}</span>}
                  {a.label}
                  {chosen && <span className="sr-only">, chosen</span>}
                  {a.recommended && (
                    <>
                      <span className="sr-only">, recommended</span>
                      <span className="chat-prompt-chip" aria-hidden="true">
                        recommended
                      </span>
                    </>
                  )}
                </li>
              );
            })}
          </ul>
        )}
        <p data-testid="prompt-summary" className="chat-prompt-muted text-xs font-mono">
          {summary}
        </p>
      </div>
    );
  }
  if (prompt.actions.length === 0) return null;

  const disabled = status !== 'open' || !onReply || phase !== 'idle';
  const feedbackText = feedback.trim();
  const stacked = prompt.kind !== 'gate';

  const send = async (text: string) => {
    if (!onReply) return;
    setPhase('sending');
    setError(null);
    try {
      await onReply(text);
      setPhase('sent');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send reply');
      setPhase('idle');
    }
  };

  const choose = (action: PromptAction, button: HTMLButtonElement) => {
    returnFocusRef.current = button;
    if (action.feedback) {
      setFeedbackFor(action);
      return;
    }
    void send(action.reply);
  };

  const sendFeedback = () => {
    if (!feedbackFor || feedbackText === '' || disabled) return;
    void send(`${feedbackFor.reply} ${feedbackText}`);
  };

  const cancelFeedback = () => {
    setFeedbackFor(null);
    setFeedback('');
    returnFocusRef.current?.focus();
  };

  return (
    <div className="mt-2 flex flex-col gap-2" data-prompt-kind={prompt.kind}>
      <div
        role="group"
        aria-label="Reply options"
        aria-busy={phase === 'sending'}
        data-layout={stacked ? 'stacked' : 'compact'}
        className={stacked ? 'flex flex-col gap-2' : 'flex flex-wrap gap-2'}
      >
        {prompt.actions.map((a, i) => {
          const primary = recommendedPrimary && a.recommended && !stacked;
          return (
            <button
              key={`${i}:${a.label}`}
              type="button"
              className={
                stacked ? 'chat-prompt-option' : primary ? 'bf-btn-primary bf-btn-sm' : 'bf-btn-ghost bf-btn-sm'
              }
              disabled={disabled}
              onClick={(e) => choose(a, e.currentTarget)}
            >
              {a.label}
              {a.recommended && (
                <>
                  <span className="sr-only">, recommended</span>
                  {!primary && (
                    <span className="chat-prompt-chip" aria-hidden="true">
                      recommended
                    </span>
                  )}
                </>
              )}
            </button>
          );
        })}
      </div>
      {feedbackFor && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor={feedbackId} className="chat-prompt-muted text-xs">
            {feedbackFor.ask ?? `Feedback for ${feedbackFor.label}`}
          </label>
          <textarea
            ref={textareaRef}
            id={feedbackId}
            className="bf-input"
            rows={2}
            value={feedback}
            maxLength={MAX_MESSAGE_LENGTH - feedbackFor.reply.length - 1}
            disabled={disabled}
            onChange={(e) => setFeedback(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.preventDefault();
                cancelFeedback();
              } else if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                sendFeedback();
              }
            }}
          />
          <div className="flex gap-2">
            <button
              type="button"
              className="bf-btn-ghost bf-btn-sm"
              disabled={disabled || feedbackText === ''}
              onClick={sendFeedback}
            >
              Send
            </button>
            <button
              type="button"
              className="bf-btn-ghost bf-btn-sm"
              disabled={phase === 'sending'}
              onClick={cancelFeedback}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
      {error && (
        <div
          role="alert"
          className="text-xs px-2 py-1 rounded font-mono"
          style={{ background: 'var(--bg-red)', color: 'var(--fg)' }}
        >
          {error}
        </div>
      )}
    </div>
  );
}
