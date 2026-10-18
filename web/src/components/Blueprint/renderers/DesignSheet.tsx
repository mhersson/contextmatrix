import { BodyMarkdown } from '../BodyMarkdown';
import { BasisChips } from '../Chips';
import { DiffText } from '../DiffText';
import { RevisionCloud } from '../RevisionCloud';
import { parseDesign, stageAssumptions, type Decision, type DesignSlice } from '../artifactData';
import { changedById } from '../revisions';
import { AssumptionsTable } from './parts';
import type { RendererProps } from './types';

// Design changes are found per decision id; the written text below per paragraph.
export function DesignSheet({ artifact, previous, mode }: RendererProps) {
  const slices = parseDesign(artifact.data);
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!slices || slices.length === 0) return fullText;
  const before = previous ? (parseDesign(previous.data) ?? []) : [];
  const beforeById = new Map(before.flatMap((s) => s.decisions).map((d) => [d.id, d]));
  const changed =
    mode === 'clouds' ? changedById([...beforeById.values()], slices.flatMap((s) => s.decisions)) : new Set<string>();
  return (
    <>
      {slices.map((s) => (
        <section key={s.id}>
          <h3>
            {s.id}
            {s.projects.length > 0 && <span className="bp-h-sub"> {s.projects.join(', ')}</span>}
          </h3>
          {s.decisions.map((d) => {
            const block = (
              <DecisionBlock decision={d} before={mode === 'compare' ? (beforeById.get(d.id) ?? null) : null} compare={mode === 'compare'} />
            );
            return changed.has(d.id) ? (
              <RevisionCloud key={d.id} version={artifact.version}>
                {block}
              </RevisionCloud>
            ) : (
              <div key={d.id}>{block}</div>
            );
          })}
          {mode === 'compare' &&
            removedDecisions(before, s).map((d) => (
              <div key={d.id} className="bp-dec bp-dec--removed">
                <del>
                  {d.id} {d.text}
                </del>
              </div>
            ))}
          {s.contracts.length > 0 && (
            <>
              <h4>Contracts</h4>
              <ul className="bp-list">
                {s.contracts.map((c, i) => (
                  <li key={i}>{c}</li>
                ))}
              </ul>
            </>
          )}
        </section>
      ))}
      <AssumptionsTable
        items={stageAssumptions(artifact.data)}
        previous={mode === 'clouds' && previous ? stageAssumptions(previous.data) : null}
        version={artifact.version}
      />
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written design</summary>
        {fullText}
      </details>
    </>
  );
}

function DecisionBlock({ decision: d, before, compare }: { decision: Decision; before: Decision | null; compare: boolean }) {
  return (
    <div className={`bp-dec${compare && before === null ? ' bp-dec--added' : ''}`}>
      <div className="bp-dec-head">
        <span className="bp-id">{d.id}</span> {before ? <DiffText before={before.text} after={d.text} /> : d.text}
      </div>
      <p>
        <span className="bp-dec-label">Chosen:</span> {before ? <DiffText before={before.chosen} after={d.chosen} /> : d.chosen}
      </p>
      {d.rejected.length > 0 && (
        <ul className="bp-rejected">
          {d.rejected.map((r, i) => (
            <li key={i}>
              Rejected: {r.option}
              {r.why && ` (${r.why})`}
            </li>
          ))}
        </ul>
      )}
      {d.basis && (
        <div className="bp-meta">
          <span className="bp-dec-label">Basis</span> <BasisChips basis={d.basis} />
        </div>
      )}
    </div>
  );
}

function removedDecisions(before: DesignSlice[], slice: DesignSlice): Decision[] {
  const now = new Set(slice.decisions.map((d) => d.id));
  return (before.find((s) => s.id === slice.id)?.decisions ?? []).filter((d) => !now.has(d.id));
}
