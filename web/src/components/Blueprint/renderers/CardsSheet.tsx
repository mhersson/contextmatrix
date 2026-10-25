import { BodyMarkdown } from '../BodyMarkdown';
import { labelsById, parseCards, type CardPreview, type ManualCheck } from '../artifactData';
import { IdPill } from './parts';
import { TableSection, type Column } from './TableSection';
import type { RendererProps } from './types';

const MANUAL_COLUMNS: Column<ManualCheck>[] = [
  { label: 'Step', kind: 'id', cell: (m) => <IdPill id={m.id} /> },
  { label: 'Check', kind: 'long', cell: (m) => m.text },
  { label: 'Covers', kind: 'list', cell: (m) => m.criteria.join(', ') || null },
];

export function CardsSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseCards(artifact.data);
  const summary = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!data || data.cards.length === 0) return summary;
  const names = labelsById(data.cards, data.manual);
  const orderShown = data.cards.length > 1 && data.order.length > 0;
  const manualIds = new Set(data.manual.map((m) => m.id));
  const orderedIds = new Set(orderShown ? data.order : []);
  const followups = data.followups ? new Set(data.followups) : null;
  return (
    <>
      {summary}
      {orderShown && (
        <section>
          <h3>
            Playbook order{data.title && <span className="bp-h-sub"> {data.title}</span>}
          </h3>
          <ol className="bp-order">
            {data.order.map((id) => {
              const deps = data.cards.find((c) => c.id === id)?.dependsOn ?? [];
              return (
                <li key={id}>
                  <span className="bp-id">{id}</span> {names.get(id) ?? ''}
                  {manualIds.has(id) && <span className="bp-after"> manual check</span>}
                  {deps.length > 0 && <span className="bp-after"> after {deps.join(', ')}</span>}
                </li>
              );
            })}
          </ol>
        </section>
      )}
      {data.cards.map((c) => (
        <CardBlock key={c.id} card={c} version={artifact.version} />
      ))}
      <TableSection
        title={followups ? 'After the run (follow-ups)' : 'Manual checks'}
        columns={MANUAL_COLUMNS}
        rows={data.manual.filter((m) => (followups ? followups.has(m.id) : !orderedIds.has(m.id)))}
        rowKey={(m) => m.id}
      />
    </>
  );
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

function CardBlock({ card: c, version }: { card: CardPreview; version: number }) {
  return (
    <article className={`bp-card bp-card--${c.type || 'task'}`} aria-label={`${c.id} ${c.title}`}>
      <div className="bp-card-title">
        <span className="bp-id">{c.id}</span>
        {c.title}
      </div>
      <div className="bp-meta">
        {c.project && <span className="bp-pill">{c.project}</span>}
        {c.type && <span className={`bp-pill bp-pill--${c.type}`}>{c.type}</span>}
        {c.priority && <span className={`bp-pill bp-pill--${c.priority}`}>{c.priority}</span>}
        <span>{count(c.places, 'place', 'places')}</span>
        <span>{count(c.criteria, 'criterion', 'criteria')}</span>
        <span>{c.dependsOn.length > 0 ? `after ${c.dependsOn.join(', ')}` : 'no dependencies'}</span>
      </div>
      {c.body && (
        <details>
          <summary>Card body</summary>
          <BodyMarkdown body={c.body} mode="plain" version={version} mockups />
        </details>
      )}
    </article>
  );
}
