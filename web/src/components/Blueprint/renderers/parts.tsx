import { Fragment, type ReactNode } from 'react';
import { BasisChips } from '../Chips';
import type { Assumption, CrossFinding, Outcome } from '../artifactData';
import { NoticeIcon, StepsIcon } from '../icons';
import { severityOf } from '../ledger';
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

/** Text with each non-empty backtick pair as a code element; anything unpaired stays as written. */
// eslint-disable-next-line react-refresh/only-export-components
export function codeSpans(text: string): ReactNode[] {
  return text.split(/`([^`]+)`/).map((part, i) => (i % 2 === 1 ? <code key={i}>{part}</code> : part));
}

/** A tinted box for the steps to take (yellow) or the notices to read (blue). */
export function Callout({ title, items, tone }: { title: string; items: string[]; tone: 'steps' | 'notices' }) {
  if (items.length === 0) return null;
  return (
    <section className={`bp-callout bp-callout--${tone}`}>
      <h3>
        {tone === 'steps' ? <StepsIcon /> : <NoticeIcon />}
        {title}
      </h3>
      <ul className="bp-list">
        {items.map((t, i) => (
          <li key={i}>{codeSpans(t)}</li>
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

function SeverityPill({ severity }: { severity: string }) {
  const s = severityOf({ severity, text: '' });
  return s && <span className={`bp-sev${s.level ? ` bp-sev--${s.level}` : ''}`}>{s.label}</span>;
}

const CROSS_COLUMNS: Column<CrossFinding>[] = [
  { label: 'Id', kind: 'id', cell: (f) => (f.id ? <IdPill id={f.id} /> : null) },
  { label: 'Severity', kind: 'status', cell: (f) => <SeverityPill severity={f.severity} /> },
  { label: 'Finding', kind: 'long', cell: (f) => f.text },
  { label: 'Refs', kind: 'list', cell: (f) => (f.refs.length > 0 ? <IdList ids={f.refs} /> : null) },
];

interface CrossTableProps {
  items: CrossFinding[];
  /** Ids of the findings new or changed since the previous version, when its changes are clouded. */
  changed?: ReadonlySet<string>;
  version: number;
}

/** Problems between artifacts, critical first; nothing for a version stored before readiness judged the whole blueprint. */
export function CrossTable({ items, changed, version }: CrossTableProps) {
  return (
    <TableSection
      title="Across the blueprint"
      columns={CROSS_COLUMNS}
      rows={items}
      rowKey={(f, i) => f.id || `#${i}`}
      changed={changed}
      version={version}
    />
  );
}
