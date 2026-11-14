import { useState, type ReactNode } from 'react';
import { pillTone, stateWord } from '../../../../lib/sentinel';

/** A state as a pill in its tone; the word defaults to the state's. */
export function Pill({ state, children }: { state: string; children?: ReactNode }) {
  const tone = pillTone(state);
  return (
    <span className={`snt-pill${tone === 'neutral' ? '' : ` snt-pill--${tone}`}`}>{children ?? stateWord(state)}</span>
  );
}

export function ExternalLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a className="snt-link" href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <p className="snt-empty" data-testid="sentinel-empty">
      {children}
    </p>
  );
}

export function DetailLabel({ children }: { children: ReactNode }) {
  return <h4 className="snt-label">{children}</h4>;
}

export function Heading({ children, count }: { children: ReactNode; count?: number }) {
  return (
    <h3 className="snt-heading">
      {children}
      {count !== undefined && <span className="snt-heading-count">{count}</span>}
    </h3>
  );
}

/** A compact table in the Sessions table frame; `kv` makes the first column a key column. */
export function Table({ kv, testId, children }: { kv?: boolean; testId?: string; children: ReactNode }) {
  return (
    <div className="ssn-table-wrap">
      <table className={kv ? 'snt-table snt-kv' : 'snt-table'} data-testid={testId}>
        {children}
      </table>
    </div>
  );
}

/** The header cell over the expand carets. */
export function CaretHeader() {
  return (
    <th className="snt-caret-col" style={{ width: 24 }}>
      <span className="sr-only">Details</span>
    </th>
  );
}

/** A row that opens to a detail row spanning `span` columns; `label` names the row's button. */
export function ExpandRow({
  label,
  span,
  defaultOpen = false,
  cells,
  children,
}: {
  label: string;
  span: number;
  defaultOpen?: boolean;
  cells: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <>
      <tr className={open ? 'snt-row snt-row--open' : 'snt-row'} onClick={() => setOpen((o) => !o)}>
        <td className="snt-caret-col">
          <button type="button" className="snt-caret" aria-expanded={open} aria-label={label}>
            {open ? '▼' : '▶'}
          </button>
        </td>
        {cells}
      </tr>
      {open && (
        <tr>
          <td className="snt-detail" colSpan={span}>
            {children}
          </td>
        </tr>
      )}
    </>
  );
}
