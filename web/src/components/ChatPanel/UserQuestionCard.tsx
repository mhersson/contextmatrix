import { useMemo, useState } from 'react';

export interface AskUserQuestionOption {
  label: string;
  description?: string;
}

export interface AskUserQuestionItem {
  question: string;
  header?: string;
  multiSelect?: boolean;
  options: AskUserQuestionOption[];
}

export interface AskUserQuestionPayload {
  questions: AskUserQuestionItem[];
  /** Number of questions dropped at the clamp boundary; 0 if no clamp fired. */
  truncatedQuestions: number;
  /** Per-question count of options dropped; index matches `questions`. */
  truncatedOptions: number[];
}

const MAX_QUESTIONS = 20;
const MAX_OPTIONS = 20;
const MAX_MALFORMED_PREVIEW = 200;

export interface UserQuestionCardProps {
  /** Raw JSON payload from a `user_question` LogEntry. */
  content: string;
  /** Disable answer buttons (read-only / ended sessions, stale history). */
  disabled: boolean;
  /** Called with the chosen option label(s) joined by `, ` for multi-select. */
  onAnswer: (text: string) => void | Promise<void>;
}

/**
 * Parse the AskUserQuestion JSON payload defensively. Returns null when the
 * payload is missing, malformed, or empty. A `JSON.parse` failure can be
 * either malformed JSON from the model or a transcript/broadcaster cap
 * mid-truncation; both fall through to the malformed-payload UI.
 */
function parsePayload(content: string): AskUserQuestionPayload | null {
  try {
    const parsed = JSON.parse(content) as unknown;
    if (!parsed || typeof parsed !== 'object') return null;

    const rawQuestions = (parsed as { questions?: unknown }).questions;
    if (!Array.isArray(rawQuestions) || rawQuestions.length === 0) return null;

    const truncatedQuestions = Math.max(0, rawQuestions.length - MAX_QUESTIONS);
    const clampedQuestions = rawQuestions.slice(0, MAX_QUESTIONS);

    const questions: AskUserQuestionItem[] = [];
    const truncatedOptions: number[] = [];

    for (const raw of clampedQuestions) {
      if (!raw || typeof raw !== 'object') continue;
      const q = raw as Partial<AskUserQuestionItem> & { options?: unknown };
      const optionsArray = Array.isArray(q.options) ? q.options : [];
      const truncatedOpts = Math.max(0, optionsArray.length - MAX_OPTIONS);
      const clampedOpts = optionsArray.slice(0, MAX_OPTIONS) as AskUserQuestionOption[];

      questions.push({
        question: q.question ?? '',
        header: q.header,
        multiSelect: q.multiSelect,
        options: clampedOpts,
      });
      truncatedOptions.push(truncatedOpts);
    }

    if (questions.length === 0) return null;

    return { questions, truncatedQuestions, truncatedOptions };
  } catch {
    return null;
  }
}

export function UserQuestionCard({ content, disabled, onAnswer }: UserQuestionCardProps) {
  const payload = useMemo(() => parsePayload(content), [content]);

  if (!payload) {
    const preview = content.length > MAX_MALFORMED_PREVIEW
      ? content.slice(0, MAX_MALFORMED_PREVIEW) + '…'
      : content;
    return (
      <div
        className="pl-3 border-l-2 text-sm font-mono leading-relaxed break-words"
        style={{ borderLeftColor: 'var(--red)', color: 'var(--red)' }}
        data-testid="user-question-malformed"
      >
        AskUserQuestion: malformed payload — {preview}
      </div>
    );
  }

  return (
    <div
      role="status"
      aria-live="polite"
      aria-label="Claude is asking a question"
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
      {payload.questions.map((q, idx) => {
        const QuestionComponent = q.multiSelect ? MultiSelectQuestion : SingleSelectQuestion;
        return (
          <QuestionComponent
            key={idx}
            item={q}
            disabled={disabled}
            onAnswer={onAnswer}
            truncatedOptions={payload.truncatedOptions[idx]}
          />
        );
      })}
      {payload.truncatedQuestions > 0 && (
        <div
          className="text-xs font-mono italic"
          style={{ color: 'var(--grey1)' }}
        >
          [{payload.truncatedQuestions} more question{payload.truncatedQuestions === 1 ? '' : 's'} truncated]
        </div>
      )}
    </div>
  );
}

interface QuestionProps {
  item: AskUserQuestionItem;
  disabled: boolean;
  onAnswer: (text: string) => void | Promise<void>;
  truncatedOptions: number;
}

function QuestionHeader({ header }: { header?: string }) {
  if (!header) return null;
  return (
    <span
      className="inline-block text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-mono"
      style={{ backgroundColor: 'var(--bg3)', color: 'var(--purple)' }}
    >
      {header}
    </span>
  );
}

function TruncatedOptionsLine({ count }: { count: number }) {
  if (count === 0) return null;
  return (
    <div
      className="text-xs font-mono italic px-2"
      style={{ color: 'var(--grey1)' }}
    >
      [{count} more option{count === 1 ? '' : 's'} truncated]
    </div>
  );
}

function SingleSelectQuestion({ item, disabled, onAnswer, truncatedOptions }: QuestionProps) {
  return (
    <fieldset className="space-y-2 border-0 p-0 m-0">
      <QuestionHeader header={item.header} />
      <legend className="text-sm" style={{ color: 'var(--fg)' }}>
        {item.question}
      </legend>
      <div className="flex flex-col gap-1.5">
        {item.options.map((opt, idx) => (
          <button
            key={idx}
            type="button"
            data-testid={`user-question-option-${idx}`}
            onClick={() => void onAnswer(opt.label)}
            disabled={disabled}
            className="text-left px-2 py-1.5 rounded border text-sm transition-colors hover:border-[var(--aqua)] focus-visible:border-[var(--aqua)] focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
            style={{ borderColor: 'var(--bg3)', backgroundColor: 'transparent', color: 'var(--fg)' }}
          >
            <span style={{ color: 'var(--fg)' }}>{opt.label}</span>
            {opt.description && (
              <span className="block text-xs" style={{ color: 'var(--grey1)' }}>
                {opt.description}
              </span>
            )}
          </button>
        ))}
      </div>
      <TruncatedOptionsLine count={truncatedOptions} />
    </fieldset>
  );
}

function MultiSelectQuestion({ item, disabled, onAnswer, truncatedOptions }: QuestionProps) {
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const toggle = (idx: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) {
        next.delete(idx);
      } else {
        next.add(idx);
      }
      return next;
    });
  };

  const sendMulti = () => {
    if (disabled || selected.size === 0) return;
    const answer = Array.from(selected)
      .sort((a, b) => a - b)
      .map((i) => item.options[i].label)
      .join(', ');
    void onAnswer(answer);
  };

  return (
    <fieldset className="space-y-2 border-0 p-0 m-0">
      <QuestionHeader header={item.header} />
      <legend className="text-sm" style={{ color: 'var(--fg)' }}>
        {item.question}
      </legend>
      <div className="flex flex-col gap-1.5">
        {item.options.map((opt, idx) => {
          const checked = selected.has(idx);
          return (
            <label
              key={idx}
              data-testid={`user-question-option-${idx}`}
              className="flex items-start gap-2 px-2 py-1.5 rounded cursor-pointer border text-sm disabled:cursor-not-allowed"
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
                onChange={() => toggle(idx)}
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
        })}
      </div>
      <TruncatedOptionsLine count={truncatedOptions} />
      <button
        type="button"
        onClick={sendMulti}
        disabled={disabled || selected.size === 0}
        className="bf-btn-ghost bf-btn-sm disabled:opacity-50"
        style={{
          color: 'var(--aqua)',
          borderColor: 'color-mix(in oklab, var(--aqua) 35%, transparent)',
        }}
      >
        Send ({selected.size})
      </button>
    </fieldset>
  );
}
