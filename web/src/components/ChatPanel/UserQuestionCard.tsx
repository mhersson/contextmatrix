import { useState } from 'react';

interface AskUserQuestionOption {
  label: string;
  description?: string;
}

interface AskUserQuestionItem {
  question: string;
  header?: string;
  multiSelect?: boolean;
  options: AskUserQuestionOption[];
}

interface AskUserQuestionPayload {
  questions: AskUserQuestionItem[];
}

export interface UserQuestionCardProps {
  /** Raw JSON payload from a `user_question` LogEntry. */
  content: string;
  /** Disable answer buttons (read-only / ended sessions, stale history). */
  disabled: boolean;
  /** Called with the chosen option label(s) joined by `, ` for multi-select. */
  onAnswer: (text: string) => void | Promise<void>;
}

function parsePayload(content: string): AskUserQuestionPayload | null {
  try {
    const parsed = JSON.parse(content) as unknown;
    if (!parsed || typeof parsed !== 'object') return null;
    const questions = (parsed as { questions?: unknown }).questions;
    if (!Array.isArray(questions) || questions.length === 0) return null;
    return parsed as AskUserQuestionPayload;
  } catch {
    return null;
  }
}

export function UserQuestionCard({ content, disabled, onAnswer }: UserQuestionCardProps) {
  const payload = parsePayload(content);

  if (!payload) {
    return (
      <div
        className="pl-3 border-l-2 text-sm font-mono leading-relaxed break-words"
        style={{ borderLeftColor: 'var(--red)', color: 'var(--red)' }}
        data-testid="user-question-malformed"
      >
        AskUserQuestion: malformed payload — {content}
      </div>
    );
  }

  return (
    <div
      className="rounded-md border-l-2 px-3 py-2 space-y-3"
      style={{ backgroundColor: 'var(--bg-purple)', borderLeftColor: 'var(--purple)', color: 'var(--fg)' }}
      data-testid="user-question-card"
    >
      <div
        className="text-[10px] uppercase tracking-wider font-mono"
        style={{ color: 'var(--grey1)' }}
      >
        Claude is asking
      </div>
      {payload.questions.map((q, idx) => (
        <QuestionItem key={idx} item={q} disabled={disabled} onAnswer={onAnswer} />
      ))}
    </div>
  );
}

interface QuestionItemProps {
  item: AskUserQuestionItem;
  disabled: boolean;
  onAnswer: (text: string) => void | Promise<void>;
}

function QuestionItem({ item, disabled, onAnswer }: QuestionItemProps) {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const multi = !!item.multiSelect;

  const toggle = (label: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(label)) {
        next.delete(label);
      } else {
        next.add(label);
      }
      return next;
    });
  };

  const sendMulti = () => {
    if (disabled || selected.size === 0) return;
    const answer = Array.from(selected).join(', ');
    void onAnswer(answer);
  };

  return (
    <div className="space-y-2">
      {item.header && (
        <span
          className="inline-block text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-mono"
          style={{ backgroundColor: 'var(--bg3)', color: 'var(--purple)' }}
        >
          {item.header}
        </span>
      )}
      <div className="text-sm" style={{ color: 'var(--fg)' }}>
        {item.question}
      </div>
      <div className="flex flex-col gap-1.5">
        {item.options.map((opt) => {
          if (multi) {
            const checked = selected.has(opt.label);
            return (
              <label
                key={opt.label}
                className="flex items-start gap-2 px-2 py-1.5 rounded cursor-pointer border text-sm"
                style={{
                  borderColor: checked ? 'var(--aqua)' : 'var(--bg3)',
                  backgroundColor: checked ? 'var(--bg-blue)' : 'transparent',
                  cursor: disabled ? 'not-allowed' : 'pointer',
                  opacity: disabled ? 0.6 : 1,
                }}
              >
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={disabled}
                  onChange={() => toggle(opt.label)}
                  className="mt-0.5"
                />
                <span className="flex flex-col">
                  <span style={{ color: 'var(--fg)' }}>{opt.label}</span>
                  {opt.description && (
                    <span className="text-xs" style={{ color: 'var(--grey1)' }}>
                      {opt.description}
                    </span>
                  )}
                </span>
              </label>
            );
          }

          return (
            <button
              key={opt.label}
              type="button"
              onClick={() => void onAnswer(opt.label)}
              disabled={disabled}
              className="text-left px-2 py-1.5 rounded border text-sm transition-colors"
              style={{
                borderColor: 'var(--bg3)',
                backgroundColor: 'transparent',
                color: 'var(--fg)',
                cursor: disabled ? 'not-allowed' : 'pointer',
                opacity: disabled ? 0.6 : 1,
              }}
              onMouseEnter={(e) => {
                if (!disabled) e.currentTarget.style.borderColor = 'var(--aqua)';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = 'var(--bg3)';
              }}
            >
              <span style={{ color: 'var(--fg)' }}>{opt.label}</span>
              {opt.description && (
                <span className="block text-xs" style={{ color: 'var(--grey1)' }}>
                  {opt.description}
                </span>
              )}
            </button>
          );
        })}
      </div>
      {multi && (
        <button
          type="button"
          onClick={sendMulti}
          disabled={disabled || selected.size === 0}
          className="bf-btn-ghost bf-btn-sm"
          style={{
            color: 'var(--aqua)',
            borderColor: 'color-mix(in oklab, var(--aqua) 35%, transparent)',
            opacity: disabled || selected.size === 0 ? 0.5 : 1,
          }}
        >
          Send ({selected.size})
        </button>
      )}
    </div>
  );
}
