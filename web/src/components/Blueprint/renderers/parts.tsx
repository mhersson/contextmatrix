import { Fragment } from 'react';
import { BasisChips } from '../Chips';
import type { Assumption, Outcome } from '../artifactData';
import { changedById } from '../revisions';
import { assumptionKey, changedAssumptions, numbered, type NumberedAssumption } from './rows';
import { TableSection, type Column } from './TableSection';

export function IdPill({ id }: { id: string }) {
  return <span className="bp-id">{id}</span>;
}

/** Ids as pills; the spaces between them let a note cell wrap, and a list cell's flex row drops them. */
export function IdList({ ids }: { ids: readonly string[] }) {
  return (
    <>
      {ids.map((id, i) => (
        <Fragment key={`${i}:${id}`}>
          {i > 0 && ' '}
          <IdPill id={id} />
        </Fragment>
      ))}
    </>
  );
}

/** An assumption's id, or its place on the sheet when the stage gave it none. */
export function AssumptionNumber({ row }: { row: NumberedAssumption }) {
  return row.a.id ? <IdPill id={row.a.id} /> : <span className="bp-n">{row.n}</span>;
}

const ASSUMPTION_COLUMNS: Column<NumberedAssumption>[] = [
  { label: '#', kind: 'id', cell: (row) => <AssumptionNumber row={row} /> },
  { label: 'Assumption', kind: 'long', cell: (row) => row.a.text },
  { label: 'Basis', kind: 'list', cell: (row) => (row.a.basis ? <BasisChips basis={row.a.basis} /> : null) },
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

const OUTCOME_COLUMNS: Column<Outcome>[] = [
  { label: 'Id', kind: 'id', cell: (o) => (o.id ? <IdPill id={o.id} /> : null) },
  { label: 'Outcome', kind: 'long', cell: (o) => o.text },
  { label: 'Quality', kind: 'short', cell: (o) => o.quality || null },
];

interface OutcomesTableProps {
  items: Outcome[];
  /** The previous version's outcomes, when its changes are clouded. */
  previous: Outcome[] | null;
  version: number;
}

/** A stage's measurable outcomes; nothing for a version stored before outcomes existed. */
export function OutcomesTable({ items, previous, version }: OutcomesTableProps) {
  return (
    <TableSection
      title="Outcomes"
      columns={OUTCOME_COLUMNS}
      rows={items}
      rowKey={(o, i) => o.id || `#${i}`}
      changed={previous ? changedById(previous, items) : undefined}
      version={version}
    />
  );
}
