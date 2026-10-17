import type { Round } from './parseRound';
import { answerLabel, storedAnswer, storedDecision } from './roundDraft';
import { countText, personaPhrase } from './roundView';

/** A closed or superseded round in the transcript: each question with the answer the architect stored. */
export function RoundSummary({ round, superseded }: { round: Round; superseded: boolean }) {
  const who = personaPhrase(round.persona);
  return (
    <div className="flex justify-start">
      <div className="qb-summary" role="group" aria-label={`Answered questions from ${who}`}>
        <p className="qb-summary-head">
          <b>{countText(round)}</b> from {who}
          {round.responder ? ', answered by the responder' : ''}
          {superseded ? ', no longer open' : ''}
        </p>
        <ul>
          {round.questions.map((q) => {
            const a = storedAnswer(round, q);
            return (
              <li key={`q${q.number}`}>
                <span className="qb-summary-q">
                  Q{q.number}. {q.text}
                </span>
                <span aria-hidden="true"> → </span>
                <span className="sr-only">: </span>
                {a ? answerLabel(q, a) : <span className="missing">not answered</span>}
              </li>
            );
          })}
          {round.assumptions.map((as, i) => {
            const d = storedDecision(round, as.id);
            return (
              <li key={as.id}>
                <span className="qb-summary-q">
                  Assumption {i + 1}. {as.text}
                </span>
                <span aria-hidden="true"> → </span>
                <span className="sr-only">: </span>
                {d === null ? <span className="missing">not decided</span> : d ? 'accepted' : 'turned into a question'}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
