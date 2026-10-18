import type { ReactNode } from 'react';
import { RevisionMark } from '../RevisionCloud';

/**
 * How a column sits: id and status stay narrow and, on a narrow sheet, inline at the top of the row;
 * long takes the rest and wraps; short holds one value and never wraps; list wraps only between its
 * values; note wraps. Short, list and note cells read "Label: value" on a narrow sheet.
 */
export type ColumnKind = 'id' | 'status' | 'long' | 'short' | 'list' | 'note';

export interface Column<T> {
  label: string;
  kind: ColumnKind;
  cell: (row: T) => ReactNode;
}

interface TableSectionProps<T> {
  title: string;
  /** Muted words after the row count. */
  sub?: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T, index: number) => string;
  /** Row keys to mark with a revision cloud of `version`. */
  changed?: ReadonlySet<string>;
  version?: number;
}

// Adjacent chips and pills leave no break opportunity, so a list's values wrap in a flex row instead.
function Cell({ kind, children }: { kind: ColumnKind; children: ReactNode }) {
  if (kind !== 'list' || children == null) return children;
  return <span className="bp-tags">{children}</span>;
}

export function TableSection<T>({ title, sub, columns, rows, rowKey, changed, version }: TableSectionProps<T>) {
  if (rows.length === 0) return null;
  return (
    <section>
      <h3>
        {title} <span className="bp-count">{rows.length}</span>
        {sub && <span className="bp-h-sub"> {sub}</span>}
      </h3>
      {/* Explicit roles keep the table semantics WebKit drops when a narrow sheet restyles the rows. */}
      <table role="table" className="bp-table bp-table--rows">
        <thead role="rowgroup">
          <tr role="row">
            {columns.map((c) => (
              <th key={c.label} role="columnheader" scope="col" className={`bp-col--${c.kind}`}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody role="rowgroup">
          {rows.map((row, i) => {
            const key = rowKey(row, i);
            const clouded = version !== undefined && changed?.has(key) === true;
            return (
              <tr key={key} role="row" className={clouded ? 'bp-cloud' : undefined}>
                {columns.map((c, ci) => (
                  <td key={c.label} role="cell" className={`bp-col--${c.kind}`} data-label={c.label}>
                    {clouded && ci === 0 && <RevisionMark version={version} />}
                    <Cell kind={c.kind}>{c.cell(row)}</Cell>
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
