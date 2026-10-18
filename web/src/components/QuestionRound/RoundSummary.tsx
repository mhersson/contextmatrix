import { useId, useState } from 'react';
import { DialogueIcon } from '../Blueprint/icons';
import { useBlueprintSheet } from '../Blueprint/sheetContext';
import { personaName, sliceSuffix, stageText } from '../Blueprint/stages';
import type { Round, RoundChatMessage } from './parseRound';
import { answerLabel, storedAnswer, storedDecision } from './roundDraft';
import { countText, personaPhrase } from './roundView';
import { RoundThread } from './RoundThread';

/** The thread behind a closed round's answers, collapsed until asked for. */
function SummaryChat({ chat }: { chat: readonly RoundChatMessage[] }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <div className="qb-card-chat">
      <button type="button" aria-expanded={open} aria-controls={open ? id : undefined} onClick={() => setOpen((o) => !o)}>
        <span aria-hidden="true">{open ? '▾' : '▸'}</span> Chat about these questions ({chat.length})
      </button>
      {open && (
        <div className="qb-chat" id={id}>
          <RoundThread chat={chat} />
        </div>
      )}
    </div>
  );
}

function titleText(round: Round, suffix: string): string {
  const kind = round.questions.length > 0 ? 'questions' : 'assumptions';
  const stage = stageText(round.stage).name;
  return (stage ? `${stage} ${kind}` : kind[0].toUpperCase() + kind.slice(1)) + suffix;
}

/** What became of the round, from the answers and decisions the architect stored. */
function outcomeText(round: Round, superseded: boolean): string {
  if (superseded) return 'no longer open';
  const who = round.responder ? 'the responder' : 'you';
  const decisions = round.assumptions.map((a) => storedDecision(round, a.id));
  const accepted = decisions.filter((d) => d === true).length;
  if (round.questions.length === 0 && decisions.length > 0 && accepted === decisions.length) return `all accepted by ${who}`;
  const parts: string[] = [];
  if (round.questions.length > 0) {
    const n = round.questions.filter((q) => storedAnswer(round, q) !== null).length;
    parts.push(n === round.questions.length ? `answered by ${who}` : `${n} of ${round.questions.length} answered`);
  }
  if (decisions.length > 0) {
    const asked = decisions.filter((d) => d === false).length;
    const open = decisions.length - accepted - asked;
    parts.push([accepted > 0 && `${accepted} accepted`, asked > 0 && `${asked} asked`, open > 0 && `${open} not decided`].filter(Boolean).join(', '));
  }
  return parts.join(' · ');
}

const DECISION = {
  accepted: 'accepted',
  asked: 'turned into a question',
  undecided: 'not decided',
} as const;

function Questions({ round, labelId }: { round: Round; labelId: string }) {
  const who = round.responder ? 'Responder' : 'You';
  return (
    <>
      <p className="qb-card-sec" id={labelId}>
        Questions
      </p>
      <ul aria-labelledby={labelId}>
        {round.questions.map((q) => {
          const a = storedAnswer(round, q);
          return (
            <li key={`q${q.number}`} className="qb-card-row">
              <span className="qb-card-num">Q{q.number}</span>
              <div className="qb-card-main">
                {/* parseRound titles an untitled question Q<n>, which the number already shows. */}
                {q.title !== `Q${q.number}` && <div className="qb-card-qtitle">{q.title}</div>}
                <div className="qb-card-qtext">{q.text}</div>
                {a ? (
                  <p className="qb-card-ans">
                    <span className="qb-card-who">{who}</span>
                    <span className="sr-only">: </span>
                    {answerLabel(q, a)}
                  </p>
                ) : (
                  <p className="qb-card-ans missing">not answered</p>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </>
  );
}

function Assumptions({ round, labelId }: { round: Round; labelId: string }) {
  return (
    <>
      <p className="qb-card-sec" id={labelId}>
        Assumptions
      </p>
      <ul aria-labelledby={labelId} className="qb-card-asms">
        {round.assumptions.map((as, i) => {
          const d = storedDecision(round, as.id);
          const kind = d === null ? 'undecided' : d ? 'accepted' : 'asked';
          return (
            <li key={as.id} className="qb-card-row">
              <span className="qb-card-num">A{i + 1}</span>
              <span className="qb-card-main">{as.text}</span>
              <span className="sr-only">: </span>
              <span className={`qb-card-chip ${kind}`}>{DECISION[kind]}</span>
            </li>
          );
        })}
      </ul>
    </>
  );
}

/** A closed or superseded round in the transcript: a card that opens in place to each question with its stored answer. */
export function RoundSummary({ round, superseded }: { round: Round; superseded: boolean }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const suffix = sliceSuffix(useBlueprintSheet()?.slices ?? [], round.slice);
  const note = [personaName(round.persona), outcomeText(round, superseded)].filter(Boolean).join(' · ');
  return (
    <div className="flex justify-start">
      {/* The transcript is a polite live region; opening the card must not read every answer aloud. */}
      <div
        className={`qb-card${open ? ' open' : ''}`}
        role="group"
        aria-label={`Answered questions from ${personaPhrase(round.persona)}`}
        aria-live="off"
      >
        <button
          type="button"
          className="qb-card-head"
          aria-expanded={open}
          aria-controls={open ? `${id}-body` : undefined}
          onClick={() => setOpen((o) => !o)}
        >
          <DialogueIcon />
          <span className="qb-card-text">
            <b>{titleText(round, suffix)}</b> <span className="qb-card-count">{countText(round, ', ')}</span>
            <span className="qb-card-note">{note}</span>
          </span>
          <svg className="qb-card-chev" width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth={1.4} aria-hidden="true">
            <path d="M3.5 1.5 7 5 3.5 8.5" />
          </svg>
        </button>
        {open && (
          <div className="qb-card-body" id={`${id}-body`}>
            {round.questions.length > 0 && <Questions round={round} labelId={`${id}-q`} />}
            {round.assumptions.length > 0 && <Assumptions round={round} labelId={`${id}-a`} />}
            {round.chat.length > 0 && <SummaryChat chat={round.chat} />}
          </div>
        )}
      </div>
    </div>
  );
}
