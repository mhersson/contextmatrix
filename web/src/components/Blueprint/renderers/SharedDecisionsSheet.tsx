import { BodyMarkdown } from '../BodyMarkdown';
import { parseSharedDecisions, stageAssumptions } from '../artifactData';
import { changedById } from '../revisions';
import { ContractList, DecisionList } from './decisions';
import { AssumptionsTable } from './parts';
import type { RendererProps } from './types';

export function SharedDecisionsSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseSharedDecisions(artifact.data);
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!data || data.decisions.length === 0) return fullText;
  const before = previous ? (parseSharedDecisions(previous.data)?.decisions ?? []) : [];
  const now = new Set(data.decisions.map((d) => d.id));
  const changed = mode === 'clouds' ? changedById(before, data.decisions) : new Set<string>();
  return (
    <>
      <section>
        <DecisionList
          decisions={data.decisions}
          beforeById={new Map(before.map((d) => [d.id, d]))}
          changed={changed}
          removed={before.filter((d) => !now.has(d.id))}
          mode={mode}
          version={artifact.version}
        />
        <ContractList contracts={data.contracts} />
      </section>
      <AssumptionsTable
        items={data.assumptions}
        previous={mode === 'clouds' && previous ? stageAssumptions(previous.data) : null}
        version={artifact.version}
      />
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written shared decisions</summary>
        {fullText}
      </details>
    </>
  );
}
