import type { Ref } from 'react';
import { ByteCount } from '../ChatPanel/ByteCount';
import { ROUND_ANSWER_MAX_BYTES, utf8Length } from '../ChatPanel/chatEntryUtils';
import { OptionRow } from './OptionRow';
import type { Round, RoundQuestion } from './parseRound';
import { answerLabel, effectiveAnswer, storedAnswer, type RoundDraft } from './roundDraft';
import type { RoundMode } from './roundView';

interface QuestionPaneProps {
  round: Round;
  question: RoundQuestion;
  draft: RoundDraft;
  /** The own-answer input's text. */
  own: string;
  mode: RoundMode;
  /** The expanded options' 1-based positions. */
  expanded: readonly number[];
  onOption: (option: number) => void;
  onToggle: (option: number) => void;
  onOwn: (text: string) => void;
  ownRef: Ref<HTMLInputElement>;
}

export function QuestionPane({ round, question, draft, own, mode, expanded, onOption, onToggle, onOwn, ownRef }: QuestionPaneProps) {
  const stored = storedAnswer(round, question);
  const chosen = effectiveAnswer(round, draft, question);
  const n = question.number;
  return (
    <>
      <p className="qb-q">
        Q{n}. {question.text}
      </p>
      <p className="qb-why">
        {stored
          ? `Answered: ${answerLabel(question, stored)}.${mode === 'open' ? ' Pick another option to change it.' : ''}`
          : question.why}
      </p>
      <div role="group" aria-label={`Options for Q${n}`}>
        {question.options.map((o, i) => (
          <OptionRow
            key={i}
            n={i + 1}
            option={o}
            recommended={question.recommended === i + 1}
            selected={chosen?.option === i + 1}
            expanded={expanded.includes(i + 1)}
            researchOff={round.researchOff}
            onClick={() => onOption(i + 1)}
            onToggle={() => onToggle(i + 1)}
          />
        ))}
        <div className={`qb-opt qb-other${chosen?.text !== undefined ? ' sel' : ''}`}>
          <div className="qb-opt-top">
            <span className="qb-n">{question.options.length + 1}</span>
            <span className="qb-title">Type your own answer</span>
          </div>
          <div className="qb-detail">
            <input
              ref={ownRef}
              className="bf-input qb-input"
              data-own-answer=""
              aria-label={`Your own answer to Q${n}`}
              placeholder="Your answer to this question"
              value={own}
              disabled={mode !== 'open'}
              onChange={(e) => onOwn(e.target.value)}
            />
            <ByteCount bytes={utf8Length(own.trim())} max={ROUND_ANSWER_MAX_BYTES} className="mt-1" />
          </div>
        </div>
      </div>
    </>
  );
}
