import { BodyMarkdown } from '../BodyMarkdown';
import { parseAcceptance, type AcceptanceSlice, type Criterion, type ManualCheck } from '../artifactData';
import { changedById } from '../revisions';
import { IdPill } from './parts';
import { TableSection, type Column } from './TableSection';
import type { RendererProps } from './types';

interface SliceCheck extends ManualCheck {
  slice: string;
}

const CRITERION_COLUMNS: Column<Criterion>[] = [
  { label: 'Id', kind: 'id', cell: (c) => <IdPill id={c.id} /> },
  { label: 'Criterion', kind: 'long', cell: (c) => c.text },
  { label: 'Project', kind: 'short', cell: (c) => c.project },
];

const MANUAL_COLUMNS: Column<SliceCheck>[] = [
  { label: 'Id', kind: 'id', cell: (m) => <IdPill id={m.id} /> },
  { label: 'Slice', kind: 'short', cell: (m) => m.slice },
  { label: 'Check', kind: 'long', cell: (m) => m.text },
];

const manualOf = (slices: AcceptanceSlice[]): SliceCheck[] => slices.flatMap((s) => s.manual.map((m) => ({ ...m, slice: s.id })));

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
  return (
    <>
      {slices.map((s) => (
        <TableSection
          key={s.id}
          title={s.id}
          sub="criteria"
          columns={CRITERION_COLUMNS}
          rows={s.criteria}
          rowKey={(c) => c.id}
          changed={changedCriteria}
          version={artifact.version}
        />
      ))}
      <TableSection
        title="Manual checks"
        columns={MANUAL_COLUMNS}
        rows={manual}
        rowKey={(m) => m.id}
        changed={changedManual}
        version={artifact.version}
      />
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written criteria</summary>
        {fullText}
      </details>
    </>
  );
}
