import { BodyMarkdown } from '../BodyMarkdown';
import { BasisChips, Chip, StatusChip } from '../Chips';
import { parseSignoff, sourceTags, withoutTags, type Assumption, type ManualCheck, type SignoffCard } from '../artifactData';
import { stageText } from '../stages';
import { AssumptionNumber, CrossTable, IdPill, TextList } from './parts';
import { assumptionKey, numbered, type NumberedAssumption } from './rows';
import { TableSection, type Column } from './TableSection';
import type { RendererProps } from './types';

interface Step {
  id: string;
  n: number;
  card: SignoffCard | null;
  manual: ManualCheck | null;
}

const STEP_COLUMNS: Column<Step>[] = [
  { label: '#', kind: 'id', cell: (s) => <span className="bp-n">{s.n}</span> },
  { label: 'Step', kind: 'id', cell: (s) => <IdPill id={s.id} /> },
  { label: 'What', kind: 'long', cell: (s) => (s.card ? <b>{s.card.title}</b> : s.manual?.text) },
  {
    label: 'Kind',
    kind: 'status',
    cell: (s) =>
      s.card ? <Chip tone="blue">{s.card.type || 'card'}</Chip> : s.manual && <Chip tone="grey">manual</Chip>,
  },
];

const MANUAL_COLUMNS: Column<ManualCheck>[] = [
  { label: 'Step', kind: 'id', cell: (m) => <IdPill id={m.id} /> },
  { label: 'Check', kind: 'long', cell: (m) => m.text },
];

// Only a confirmed assumption rests on a human's answer; any other answer may be the responder's.
const yourAnswer = (a: Assumption) => (a.status === 'confirmed' ? a.answer : '');

const ANSWER_COLUMN: Column<NumberedAssumption> = { label: 'Your answer', kind: 'note', cell: ({ a }) => yourAnswer(a) || null };

function assumptionColumns(withAnswer: boolean): Column<NumberedAssumption>[] {
  return [
    { label: 'Id', kind: 'id', cell: (row) => <AssumptionNumber row={row} /> },
    {
      label: 'Assumption',
      kind: 'long',
      cell: ({ a }) => (
        <>
          {a.text}
          {a.status === 'verified' && withoutTags(a.evidence) && <span className="bp-cell-sub">{withoutTags(a.evidence)}</span>}
        </>
      ),
    },
    ...(withAnswer ? [ANSWER_COLUMN] : []),
    { label: 'Status', kind: 'status', cell: ({ a }) => <StatusChip assumption={a} /> },
    {
      label: 'Basis',
      kind: 'list',
      cell: ({ a }) => {
        const basis = [a.basis, ...sourceTags(a.evidence)].join(' ').trim();
        return basis ? <BasisChips basis={basis} /> : null;
      },
    },
    { label: 'Stage', kind: 'short', cell: ({ a }) => (a.stage ? stageText(a.stage).name : null) },
  ];
}

export function SignoffSheet({ artifact }: RendererProps) {
  const data = parseSignoff(artifact.data);
  if (!data) return <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />;
  const order = data.order.length > 0 ? data.order : data.cards.map((c) => c.id);
  const steps = order.map(
    (id, i): Step => ({
      id,
      n: i + 1,
      card: data.cards.find((c) => c.id === id) ?? null,
      manual: data.manual.find((m) => m.id === id) ?? null,
    }),
  );
  return (
    <>
      <CrossTable items={data.cross} version={artifact.version} />
      {data.overrideReason && (
        <p className="bp-override" role="note" aria-label="Override">
          Approved over critical findings: {data.overrideReason}
        </p>
      )}
      <TableSection
        title="Run order"
        sub={data.cards.length > 1 && data.title ? `as playbook ${data.title}` : undefined}
        columns={STEP_COLUMNS}
        rows={steps}
        rowKey={(s) => `${s.n}`}
      />
      {data.preflightError && (
        <p className="bp-refused" role="note" aria-label="Preflight">
          The preflight could not run: {data.preflightError}
        </p>
      )}
      <TextList title="Blocking" items={data.blocking} tone="red" />
      <TextList title="Warnings" items={data.warnings} tone="orange" />
      {data.assumptions.length > 0 ? (
        <TableSection
          title="Assumptions"
          columns={assumptionColumns(data.assumptions.some((a) => yourAnswer(a)))}
          rows={numbered(data.assumptions)}
          rowKey={assumptionKey}
          wide
        />
      ) : (
        <section>
          <h3>Assumptions</h3>
          {data.assumptionsProse ? (
            <BodyMarkdown body={data.assumptionsProse} mode="plain" version={artifact.version} />
          ) : (
            <p className="bp-sub">None.</p>
          )}
        </section>
      )}
      <TableSection title="Manual checks after the run" columns={MANUAL_COLUMNS} rows={data.manual} rowKey={(m) => m.id} />
      <TextList title="Before the cards run" items={data.steps} />
      {artifact.body && <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />}
    </>
  );
}
