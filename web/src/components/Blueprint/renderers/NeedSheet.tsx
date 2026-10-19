import { BodyMarkdown } from '../BodyMarkdown';
import { parseNeed, stageOutcomes } from '../artifactData';
import { AssumptionsTable, OutcomesTable } from './parts';
import type { RendererProps } from './types';

export function NeedSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseNeed(artifact.data);
  const before = mode === 'clouds' && previous ? (parseNeed(previous.data)?.assumptions ?? []) : null;
  return (
    <>
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
      <OutcomesTable
        items={stageOutcomes(artifact.data)}
        previous={mode === 'clouds' && previous ? stageOutcomes(previous.data) : null}
        version={artifact.version}
      />
      {data && <AssumptionsTable items={data.assumptions} previous={before} version={artifact.version} />}
    </>
  );
}
