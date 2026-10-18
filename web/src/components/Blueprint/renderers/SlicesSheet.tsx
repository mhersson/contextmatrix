import { BodyMarkdown } from '../BodyMarkdown';
import { StatusChip } from '../Chips';
import { parseSlices, stageAssumptions, type Slice } from '../artifactData';
import { changedById } from '../revisions';
import { AssumptionNumber, IdPill } from './parts';
import { assumptionKey, changedAssumptions, numbered, type NumberedAssumption } from './rows';
import { TableSection, type Column } from './TableSection';
import type { RendererProps } from './types';

const SLICE_COLUMNS: Column<Slice>[] = [
  { label: 'Slice', kind: 'id', cell: (s) => <IdPill id={s.id} /> },
  {
    label: 'What it delivers',
    kind: 'long',
    cell: (s) => (
      <>
        <b>{s.title}</b>
        {s.value && <span className="bp-cell-sub">{s.value}</span>}
      </>
    ),
  },
  {
    label: 'Project',
    kind: 'list',
    cell: (s) =>
      s.projects.map((p) => (
        <span key={p} className="bp-pill">
          {p}
        </span>
      )),
  },
  { label: 'After', kind: 'list', cell: (s) => s.dependsOn.join(', ') || null },
];

const ASSUMPTION_COLUMNS: Column<NumberedAssumption>[] = [
  { label: '#', kind: 'id', cell: (row) => <AssumptionNumber row={row} /> },
  {
    label: 'Assumption',
    kind: 'long',
    cell: (row) => (
      <>
        {row.a.text}
        {row.a.basis && <span className="bp-cell-sub">{row.a.basis}</span>}
      </>
    ),
  },
  { label: 'Status', kind: 'status', cell: (row) => <StatusChip assumption={row.a} /> },
];

export function SlicesSheet({ artifact, previous, mode }: RendererProps) {
  const slices = parseSlices(artifact.data) ?? [];
  const assumptions = stageAssumptions(artifact.data);
  const before = mode === 'clouds' ? previous : null;
  return (
    <>
      <TableSection
        title="Slices"
        columns={SLICE_COLUMNS}
        rows={slices}
        rowKey={(s) => s.id}
        changed={before ? changedById(parseSlices(before.data) ?? [], slices) : undefined}
        version={artifact.version}
      />
      <TableSection
        title="Assumptions"
        columns={ASSUMPTION_COLUMNS}
        rows={numbered(assumptions)}
        rowKey={assumptionKey}
        changed={changedAssumptions(before ? stageAssumptions(before.data) : null, assumptions)}
        version={artifact.version}
      />
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
    </>
  );
}
