import { BodyMarkdown } from '../BodyMarkdown';
import { BasisChips, Chip, StatusChip } from '../Chips';
import {
  acceptedLabel,
  CHANGED_SINCE_ACCEPTED,
  parseSignoff,
  sourceTags,
  supersededLabel,
  withoutTags,
  type Assumption,
  type ManualCheck,
  type PlainSummary as PlainSummaryData,
  type SignoffAssumption,
  type SignoffCard,
} from '../artifactData';
import { stageText } from '../stages';
import { AssumptionNumber, Callout, CrossTable, IdPill, TextList } from './parts';
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

const ANSWER_COLUMN: Column<NumberedAssumption<SignoffAssumption>> = { label: 'Your answer', kind: 'note', cell: ({ a }) => yourAnswer(a) || null };

function assumptionColumns(withAnswer: boolean): Column<NumberedAssumption<SignoffAssumption>>[] {
  return [
    { label: 'Id', kind: 'id', cell: (row) => <AssumptionNumber row={row} /> },
    {
      label: 'Assumption',
      kind: 'long',
      cell: ({ a }) => (
        <>
          <span className={a.supersededBy ? 'bp-superseded' : undefined}>{a.text}</span>
          {a.status === 'verified' && withoutTags(a.evidence) && <span className="bp-cell-sub">{withoutTags(a.evidence)}</span>}
          {a.consequence && <span className="bp-cell-sub bp-consequence">{a.consequence}</span>}
        </>
      ),
    },
    ...(withAnswer ? [ANSWER_COLUMN] : []),
    {
      label: 'Status',
      kind: 'status',
      cell: ({ a }) => (
        <>
          <StatusChip assumption={a} />
          {a.accepted && <Chip tone="grey">{acceptedLabel(a.accepted)}</Chip>}
          {a.supersededBy && <Chip tone="grey">{supersededLabel(a.supersededBy)}</Chip>}
        </>
      ),
    },
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

const ACCEPTED_COLUMNS: Column<PlainSummaryData['accepted'][number]>[] = [
  { label: 'Id', kind: 'id', cell: (c) => (c.id ? <IdPill id={c.id} /> : null) },
  { label: 'Consequence', kind: 'long', cell: (c) => c.text },
  { label: 'Status', kind: 'status', cell: (c) => (c.changed ? <Chip tone="orange">{CHANGED_SINCE_ACCEPTED}</Chip> : null) },
];

function PlainSummary({ summary }: { summary: PlainSummaryData }) {
  return (
    <>
      <TextList title="What will be built" items={summary.build} />
      <TextList title="What you can do with it" items={summary.canDo} />
      <TableSection title="Consequences you accepted" columns={ACCEPTED_COLUMNS} rows={summary.accepted} rowKey={(c, i) => `${c.id}#${i}`} />
    </>
  );
}

/** Inserts the card's title after the id of an "<id>: <finding>" entry; other entries stay as written. */
function withCardTitle(entry: string, cards: SignoffCard[]): string {
  const at = entry.indexOf(': ');
  if (at < 0) return entry;
  const id = entry.slice(0, at);
  const card = cards.find((c) => c.id === id);
  return card ? `${id} (${card.title})${entry.slice(at)}` : entry;
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
      {data.plainSummary && <PlainSummary summary={data.plainSummary} />}
      <CrossTable items={data.cross} version={artifact.version} />
      <TextList title="Cards not ready" tone="red" items={data.notReady.map((e) => withCardTitle(e, data.cards))} />
      {data.overrideReason && (
        <p className="bp-override" role="note" aria-label="Override">
          Approved over open findings: {data.overrideReason}
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
      <Callout tone="steps" title="Before the cards run" items={data.steps} />
      <Callout tone="notices" title="Notices" items={data.notices} />
      {artifact.body && <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />}
    </>
  );
}
