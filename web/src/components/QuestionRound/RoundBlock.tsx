import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import type { RoundReply } from '../../types';
import type { Round } from './parseRound';
import { AssumptionsPane } from './AssumptionsPane';
import { QuestionPane } from './QuestionPane';
import { RoundChat } from './RoundChat';
import { SubmitPane, type SendPhase } from './SubmitPane';
import { buildReply, itemCount, replySummary, storedAnswer, storedCount, storedDecision } from './roundDraft';
import { countText, personaPhrase, type RoundMode } from './roundView';
import { useRoundDraft } from './useRoundDraft';

/** "q:<number>" for a question, "assumptions" or "submit". */
type TabKey = string;

interface TabInfo {
  label: string;
  state: 'done' | 'open' | 'submit';
}

function tabKeys(round: Round, mode: RoundMode): TabKey[] {
  const keys = round.questions.map((q) => `q:${q.number}`);
  if (round.assumptions.length > 0) keys.push('assumptions');
  if (mode !== 'readonly') keys.push('submit');
  return keys;
}

function tabInfo(round: Round, key: TabKey): TabInfo {
  if (key === 'submit') return { label: 'Submit', state: 'submit' };
  if (key === 'assumptions') {
    const done = round.assumptions.every((a) => storedDecision(round, a.id) !== null);
    return { label: 'Assumptions', state: done ? 'done' : 'open' };
  }
  const q = round.questions.find((x) => `q:${x.number}` === key);
  return { label: q?.title ?? key, state: q && storedAnswer(round, q) ? 'done' : 'open' };
}

/** The highest option key any question takes: its options plus the own answer, at most 9. */
function maxKey(round: Round): number {
  return Math.min(9, Math.max(1, ...round.questions.map((q) => q.options.length + 1)));
}

/** The foot's key hint: digits only on a question tab, Enter only where a next tab exists. */
function keyHint(round: Round, mode: RoundMode, onQuestion: boolean, onLast: boolean): string {
  if (mode !== 'open') return '← → switch questions';
  if (onQuestion) return `← → switch questions · 1-${maxKey(round)} pick an option · Enter next`;
  return onLast ? '← → switch questions' : '← → switch questions · Enter next';
}

interface RoundBlockProps {
  promptId: string;
  round: Round;
  mode: RoundMode;
  /** Drawn in the transcript (a responder round) instead of docked under it. */
  inline?: boolean;
  /** Sends the answers: the readable summary and the structured reply. */
  onSendRound?: (content: string, reply: RoundReply) => Promise<void>;
  /** Sends a plain message about the round. */
  onSendChat?: (text: string) => Promise<void>;
  /** Messages sent about the round since this frame. */
  sentChat?: readonly string[];
  /** The session takes no messages; the chat box follows this, as the composer does, not the stream like answers. */
  chatDisabled?: boolean;
  /** The session the unsent picks are kept under in this browser; without it they live only as long as the block. */
  draftScope?: string;
  /** Focuses the active tab whenever it changes, as ChatComposer's focusKey does. */
  focusKey?: string | number;
}

export function RoundBlock({
  promptId,
  round,
  mode,
  inline = false,
  onSendRound,
  onSendChat,
  sentChat,
  chatDisabled = false,
  draftScope,
  focusKey,
}: RoundBlockProps) {
  const { draft, own, pickOption, typeOwn, decide, clearSaved } = useRoundDraft(
    round,
    draftScope === undefined ? undefined : { sessionID: draftScope, promptId },
  );
  const tabs = tabKeys(round, mode);
  const [picked, setPicked] = useState<TabKey | null>(null);
  const active =
    picked !== null && tabs.includes(picked) ? picked : (tabs.find((k) => tabInfo(round, k).state === 'open') ?? tabs[0]);
  const [expanded, setExpanded] = useState<Readonly<Record<number, number | null>>>({});
  const [phase, setPhase] = useState<SendPhase>('idle');
  const [sendError, setSendError] = useState<string | null>(null);
  const send = async (final: boolean) => {
    if (!onSendRound || phase !== 'idle') return;
    const reply = buildReply(promptId, round, draft, final);
    setPhase('sending');
    setSendError(null);
    try {
      await onSendRound(replySummary(round, reply), reply);
      clearSaved();
      // After a final reply the round closes when the architect republishes it; until then nothing more to send.
      setPhase(final ? 'submitted' : 'idle');
    } catch (err) {
      setSendError(err instanceof Error ? err.message : 'Failed to send answers');
      setPhase('idle');
    }
  };
  const tabRefs = useRef(new Map<TabKey, HTMLButtonElement>());
  const ownRef = useRef<HTMLInputElement>(null);
  const activeRef = useRef(active);
  const baseId = useId();

  useEffect(() => {
    activeRef.current = active;
  });
  useEffect(() => {
    if (focusKey === undefined || mode !== 'open') return;
    tabRefs.current.get(activeRef.current)?.focus();
  }, [focusKey, mode]);

  const show = (key: TabKey) => {
    setPicked(key);
    tabRefs.current.get(key)?.focus();
  };
  const move = (delta: number) => {
    const next = tabs[Math.min(tabs.length - 1, Math.max(0, tabs.indexOf(active) + delta))];
    if (next !== undefined) show(next);
  };
  const question = round.questions.find((q) => `q:${q.number}` === active) ?? null;

  const chooseOption = (number: number, option: number) => {
    const q = round.questions.find((x) => x.number === number);
    const o = q?.options[option - 1];
    if (!o) return;
    setExpanded((e) => ({ ...e, [number]: e[number] === option ? null : option }));
    if (mode === 'open') pickOption(number, option, o.title);
  };

  // Shortcuts never act on typed text: inside an input or textarea only Enter in the own answer is taken.
  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const target = e.target as HTMLElement;
    if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') {
      if (e.key === 'Enter' && target.hasAttribute('data-own-answer')) {
        e.preventDefault();
        move(1);
      }
      return;
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      move(e.key === 'ArrowLeft' ? -1 : 1);
    } else if (e.key === 'Enter' && target.getAttribute('role') === 'tab') {
      e.preventDefault();
      move(1);
    } else if (question && mode === 'open' && /^[1-9]$/.test(e.key)) {
      const n = Number(e.key);
      if (n <= question.options.length) {
        e.preventDefault();
        chooseOption(question.number, n);
      } else if (n === question.options.length + 1) {
        e.preventDefault();
        ownRef.current?.focus();
      }
    }
  };

  return (
    <section
      className={`qb${inline ? ' qb--inline' : ''}`}
      aria-label={`${countText(round)} from ${personaPhrase(round.persona)}`}
      data-mode={mode}
      onKeyDown={onKeyDown}
    >
      <div className="qb-head">
        <div>
          <b>{countText(round)}</b> from {personaPhrase(round.persona)}
        </div>
        <span>
          {storedCount(round)} of {itemCount(round)} answered
          {mode === 'readonly' ? '' : ' · your answers are kept if you leave'}
        </span>
      </div>
      <div className="qb-tabs" role="tablist" aria-label="Questions">
        {tabs.map((key) => {
          const t = tabInfo(round, key);
          return (
            <button
              key={key}
              ref={(el) => {
                if (el) tabRefs.current.set(key, el);
                else tabRefs.current.delete(key);
              }}
              type="button"
              role="tab"
              id={`${baseId}-tab-${key}`}
              aria-selected={key === active}
              aria-controls={`${baseId}-panel`}
              tabIndex={key === active ? 0 : -1}
              className={`qb-tab ${t.state}${key === active ? ' active' : ''}`}
              onClick={() => show(key)}
            >
              {t.label}
              {t.state !== 'submit' && <span className="sr-only">{t.state === 'done' ? ', answered' : ', open'}</span>}
            </button>
          );
        })}
      </div>
      <div className="qb-body" role="tabpanel" id={`${baseId}-panel`} aria-labelledby={`${baseId}-tab-${active}`}>
        {question && (
          <QuestionPane
            key={question.number}
            round={round}
            question={question}
            draft={draft}
            own={own[question.number] ?? storedAnswer(round, question)?.text ?? ''}
            mode={mode}
            expanded={expanded[question.number] ?? null}
            onOption={(option) => chooseOption(question.number, option)}
            onOwn={(text) => typeOwn(question.number, text)}
            ownRef={ownRef}
          />
        )}
        {active === 'assumptions' && <AssumptionsPane round={round} draft={draft} mode={mode} onDecide={decide} />}
        {active === 'submit' && (
          <SubmitPane round={round} draft={draft} mode={mode} phase={phase} error={sendError} onSend={(final) => void send(final)} />
        )}
      </div>
      {mode !== 'readonly' && <RoundChat round={round} disabled={chatDisabled} sent={sentChat} onSendChat={onSendChat} />}
      <div className="qb-foot">
        <span>{keyHint(round, mode, question !== null, active === tabs[tabs.length - 1])}</span>
        <span>{mode === 'readonly' ? 'Answered by the responder' : 'Composer is hidden while questions are open'}</span>
      </div>
    </section>
  );
}
