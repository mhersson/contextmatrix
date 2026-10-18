import type { Round } from './parseRound';
import { answerLabel, answeredCount, effectiveAnswer, effectiveDecision, hasUnsent, itemCount, type RoundDraft } from './roundDraft';
import type { RoundMode } from './roundView';

export type SendPhase = 'idle' | 'sending' | 'submitted';

interface SubmitPaneProps {
  round: Round;
  draft: RoundDraft;
  mode: RoundMode;
  phase: SendPhase;
  error: string | null;
  onSend: (final: boolean) => void;
}

export function SubmitPane({ round, draft, mode, phase, error, onSend }: SubmitPaneProps) {
  const total = itemCount(round);
  const answered = answeredCount(round, draft);
  const live = mode === 'open' && phase === 'idle';
  return (
    <>
      <p className="qb-q">Review your answers</p>
      <table className="qb-review">
        <tbody>
          {round.questions.map((q) => {
            const a = effectiveAnswer(round, draft, q);
            return (
              <tr key={`q${q.number}`}>
                <td>
                  Q{q.number} {q.title}
                </td>
                {a ? <td>{answerLabel(q, a)}</td> : <td className="missing">not answered</td>}
              </tr>
            );
          })}
          {round.assumptions.map((a, i) => {
            const d = effectiveDecision(round, draft, a.id);
            return (
              <tr key={a.id}>
                <td>Assumption {i + 1}</td>
                {d === null ? <td className="missing">not decided</td> : <td>{d ? 'Accepted' : 'Turn into a question'}</td>}
              </tr>
            );
          })}
        </tbody>
      </table>
      <p className="qb-actions">
        <button type="button" className="bf-btn-primary qb-btn" disabled={!live || answered < total} onClick={() => onSend(true)}>
          Submit all {total}
        </button>
        <button
          type="button"
          className="bf-btn-ghost qb-ghost qb-btn"
          disabled={!live || answered === 0 || answered === total || !hasUnsent(round, draft)}
          onClick={() => onSend(false)}
        >
          Send {answered} now, answer the rest later
        </button>
      </p>
      {phase === 'submitted' && (
        <p className="qb-why" role="status">
          Submitted. The stage continues once the architect has your answers.
        </p>
      )}
      {error && (
        <div role="alert" className="qb-error">
          {error}
        </div>
      )}
    </>
  );
}
