import { BasisChips } from '../Chips';
import type { Assumption } from '../artifactData';
import { assumptionKey, changedAssumptions, numbered, type NumberedAssumption } from './rows';
import { TableSection, type Column } from './TableSection';

export function IdPill({ id }: { id: string }) {
  return <span className="bp-id">{id}</span>;
}

/** An assumption's id, or its place on the sheet when the stage gave it none. */
export function AssumptionNumber({ row }: { row: NumberedAssumption }) {
  return row.a.id ? <IdPill id={row.a.id} /> : <span className="bp-n">{row.n}</span>;
}

const ASSUMPTION_COLUMNS: Column<NumberedAssumption>[] = [
  { label: '#', kind: 'id', cell: (row) => <AssumptionNumber row={row} /> },
  { label: 'Assumption', kind: 'long', cell: (row) => row.a.text },
  { label: 'Basis', kind: 'short', cell: (row) => <BasisChips basis={row.a.basis} /> },
];

interface AssumptionsTableProps {
  items: Assumption[];
  /** The previous version's assumptions, when its changes are clouded. */
  previous: Assumption[] | null;
  version: number;
}

/** A stage's stated assumptions with their basis. */
export function AssumptionsTable({ items, previous, version }: AssumptionsTableProps) {
  return (
    <TableSection
      title="Assumptions"
      columns={ASSUMPTION_COLUMNS}
      rows={numbered(items)}
      rowKey={assumptionKey}
      changed={changedAssumptions(previous, items)}
      version={version}
    />
  );
}

export function TextList({ title, items, tone }: { title: string; items: string[]; tone?: 'red' | 'orange' }) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className={tone ? `bp-list bp-list--${tone}` : 'bp-list'}>
        {items.map((t, i) => (
          <li key={i}>{t}</li>
        ))}
      </ul>
    </section>
  );
}
