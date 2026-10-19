import { BodyMarkdown } from '../BodyMarkdown';
import { parseAcceptance, type AcceptanceCheck, type AcceptanceSlice, type Criterion, type SweepEntry } from '../artifactData';
import { changedById } from '../revisions';
import { IdList, IdPill } from './parts';
import { TableSection, type Column } from './TableSection';
import type { RendererProps } from './types';

interface SliceCheck extends AcceptanceCheck {
  slice: string;
}

interface SweepRow extends SweepEntry {
  /** The slice and the condition, with its occurrence appended on a repeat. */
  id: string;
}

function outcomesColumn<T extends { outcomes: string[] }>(): Column<T> {
  return { label: 'Outcomes', kind: 'list', cell: (r) => (r.outcomes.length > 0 ? <IdList ids={r.outcomes} /> : null) };
}

// A table shows the column only when a row cites an outcome, so a version stored before outcomes keeps its columns.
const cites = (rows: { outcomes: string[] }[]) => rows.some((r) => r.outcomes.length > 0);

function criterionColumns(withOutcomes: boolean): Column<Criterion>[] {
  return [
    { label: 'Id', kind: 'id', cell: (c) => <IdPill id={c.id} /> },
    { label: 'Criterion', kind: 'long', cell: (c) => c.text },
    { label: 'Project', kind: 'short', cell: (c) => c.project },
    ...(withOutcomes ? [outcomesColumn<Criterion>()] : []),
  ];
}

function manualColumns(withOutcomes: boolean): Column<SliceCheck>[] {
  return [
    { label: 'Id', kind: 'id', cell: (m) => <IdPill id={m.id} /> },
    { label: 'Slice', kind: 'short', cell: (m) => m.slice },
    { label: 'Check', kind: 'long', cell: (m) => m.text },
    ...(withOutcomes ? [outcomesColumn<SliceCheck>()] : []),
  ];
}

const SWEEP_COLUMNS: Column<SweepRow>[] = [
  { label: 'Condition', kind: 'long', cell: (e) => e.condition },
  {
    label: 'Settled by',
    kind: 'note',
    cell: (e) =>
      e.settledBy.length > 0 ? <IdList ids={e.settledBy} /> : e.notApplicable ? `Not applicable: ${e.notApplicable}` : null,
  },
];

const manualOf = (slices: AcceptanceSlice[]): SliceCheck[] => slices.flatMap((s) => s.manual.map((m) => ({ ...m, slice: s.id })));

function sweepRows(s: AcceptanceSlice): SweepRow[] {
  const seen = new Map<string, number>();
  return s.sweep.map((e) => {
    const base = `${s.id}:${e.condition.trim().toLowerCase()}`;
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    return { ...e, id: n === 1 ? base : `${base}#${n}` };
  });
}

export function AcceptanceSheet({ artifact, previous, mode }: RendererProps) {
  const slices = parseAcceptance(artifact.data);
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!slices || slices.length === 0) return fullText;
  const manual = manualOf(slices);
  const before = mode === 'clouds' && previous ? (parseAcceptance(previous.data) ?? []) : null;
  const changedCriteria = before
    ? changedById(before.flatMap((s) => s.criteria), slices.flatMap((s) => s.criteria))
    : new Set<string>();
  const changedManual = before ? changedById(manualOf(before), manual) : new Set<string>();
  const changedSweep = before ? changedById(before.flatMap(sweepRows), slices.flatMap(sweepRows)) : new Set<string>();
  return (
    <>
      {slices.map((s) => (
        <TableSection
          key={s.id}
          title={s.id}
          sub="criteria"
          columns={criterionColumns(cites(s.criteria))}
          rows={s.criteria}
          rowKey={(c) => c.id}
          changed={changedCriteria}
          version={artifact.version}
        />
      ))}
      <TableSection
        title="Manual checks"
        columns={manualColumns(cites(manual))}
        rows={manual}
        rowKey={(m) => m.id}
        changed={changedManual}
        version={artifact.version}
      />
      {slices.map((s) => (
        <TableSection
          key={s.id}
          title="Edge cases"
          sub={slices.length > 1 ? s.id : undefined}
          columns={SWEEP_COLUMNS}
          rows={sweepRows(s)}
          rowKey={(e) => e.id}
          changed={changedSweep}
          version={artifact.version}
        />
      ))}
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written criteria</summary>
        {fullText}
      </details>
    </>
  );
}
