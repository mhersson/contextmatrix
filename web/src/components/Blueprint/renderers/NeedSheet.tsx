import { BodyMarkdown } from '../BodyMarkdown';
import { AssumptionList } from '../Chips';
import { parseNeed } from '../artifactData';
import type { RendererProps } from './types';

export function NeedSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseNeed(artifact.data);
  return (
    <>
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
      {data && data.assumptions.length > 0 && (
        <section>
          <h3>Assumptions</h3>
          <AssumptionList items={data.assumptions} />
        </section>
      )}
    </>
  );
}
