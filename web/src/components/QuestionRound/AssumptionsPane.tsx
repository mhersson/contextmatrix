import type { Round } from './parseRound';
import { effectiveDecision, type RoundDraft } from './roundDraft';
import type { RoundMode } from './roundView';

interface AssumptionsPaneProps {
  round: Round;
  draft: RoundDraft;
  mode: RoundMode;
  onDecide: (id: string, accept: boolean) => void;
}

export function AssumptionsPane({ round, draft, mode, onDecide }: AssumptionsPaneProps) {
  return (
    <>
      <p className="qb-q">Assumptions</p>
      <p className="qb-why">Accept each one, or turn it into a question for the next round.</p>
      <div className="qb-assume-all">
        <button
          type="button"
          className="qb-choice"
          disabled={mode !== 'open'}
          onClick={() => round.assumptions.forEach((a) => onDecide(a.id, true))}
        >
          Accept all
        </button>
      </div>
      {round.assumptions.map((a, i) => {
        const d = effectiveDecision(round, draft, a.id);
        return (
          <div key={a.id} className="qb-opt qb-assume" role="group" aria-label={`Assumption ${i + 1}`}>
            <div className="qb-opt-top">
              <span className="qb-n">{i + 1}</span>
              <span className="qb-sum">{a.text}</span>
            </div>
            <div className="qb-assume-actions">
              <button
                type="button"
                className={`qb-choice${d === true ? ' sel' : ''}`}
                aria-pressed={d === true}
                disabled={mode !== 'open'}
                onClick={() => onDecide(a.id, true)}
              >
                Accept
              </button>
              <button
                type="button"
                className={`qb-choice${d === false ? ' sel' : ''}`}
                aria-pressed={d === false}
                disabled={mode !== 'open'}
                onClick={() => onDecide(a.id, false)}
              >
                Turn into a question
              </button>
            </div>
          </div>
        );
      })}
    </>
  );
}
