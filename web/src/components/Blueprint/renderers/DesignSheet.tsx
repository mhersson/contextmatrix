import { BodyMarkdown } from '../BodyMarkdown';
import { parseDesign, stageAssumptions, type Decision, type DesignSlice } from '../artifactData';
import { changedById } from '../revisions';
import { ContractList, DecisionList } from './decisions';
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
          {/* Every per-slice design holds one entry, and the sheet already names its slice. */}
          {s.projects.length > 0 && (
            <h3>
              {slices.length > 1 && `${s.id} `}
              <span className="bp-h-sub">{s.projects.join(', ')}</span>
            </h3>
          )}
          {slices.length > 1 && s.projects.length === 0 && <h3>{s.id}</h3>}
          <DecisionList
            decisions={s.decisions}
            beforeById={beforeById}
            changed={changed}
            removed={removedDecisions(before, s)}
            mode={mode}
            version={artifact.version}
          />
          <ContractList contracts={s.contracts} />
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

function removedDecisions(before: DesignSlice[], slice: DesignSlice): Decision[] {
  const now = new Set(slice.decisions.map((d) => d.id));
  return (before.find((s) => s.id === slice.id)?.decisions ?? []).filter((d) => !now.has(d.id));
}
