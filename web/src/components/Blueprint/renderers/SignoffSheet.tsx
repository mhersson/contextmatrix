import { BodyMarkdown } from '../BodyMarkdown';
import { AssumptionList } from '../Chips';
import { labelsById, parseSignoff } from '../artifactData';
import { ManualChecks, TextList } from './parts';
import type { RendererProps } from './types';

export function SignoffSheet({ artifact }: RendererProps) {
  const data = parseSignoff(artifact.data);
  if (!data) return <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />;
  const names = labelsById(data.cards, data.manual);
  const order = data.order.length > 0 ? data.order : data.cards.map((c) => c.id);
  const several = data.cards.length > 1;
  return (
    <>
      <section>
        <h3>
          {several ? `${data.cards.length} cards` : 'One card'}
          {several && data.title && <span className="bp-h-sub"> as playbook {data.title}</span>}
        </h3>
        <ol className="bp-order">
          {order.map((id) => (
            <li key={id}>
              <span className="bp-id">{id}</span> {names.get(id) ?? ''}
            </li>
          ))}
        </ol>
      </section>
      {data.preflightError && (
        <p className="bp-refused" role="note" aria-label="Preflight">
          The preflight could not run: {data.preflightError}
        </p>
      )}
      <TextList title="Blocking" items={data.blocking} tone="red" />
      <TextList title="Warnings" items={data.warnings} tone="orange" />
      <section>
        <h3>Assumptions</h3>
        {data.assumptions.length > 0 ? (
          <AssumptionList items={data.assumptions} />
        ) : data.assumptionsProse ? (
          <BodyMarkdown body={data.assumptionsProse} mode="plain" version={artifact.version} />
        ) : (
          <p className="bp-sub">None.</p>
        )}
      </section>
      <ManualChecks title="Manual checks after the run" items={data.manual} />
      <TextList title="Before the cards run" items={data.steps} />
      {artifact.body && <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />}
    </>
  );
}
