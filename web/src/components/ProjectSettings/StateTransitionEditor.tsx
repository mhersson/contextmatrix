export interface StateTransitionEditorProps {
  states: string[];
  transitions: Record<string, string[]>;
  onChange: (next: Record<string, string[]>) => void;
}

/**
 * From/to matrix over the project's states. Rows are the state a card is
 * in, columns the state it may move to; each cell is a toggle, the diagonal
 * is inert. A table keeps the row/column headers in the accessibility tree,
 * so each cell also carries its "from to to" pair as its accessible name.
 */
export function StateTransitionEditor({ states, transitions, onChange }: StateTransitionEditorProps) {
  const toggle = (from: string, to: string) => {
    const current = transitions[from] || [];
    const next = current.includes(to) ? current.filter((s) => s !== to) : [...current, to];
    onChange({ ...transitions, [from]: next });
  };

  return (
    <>
      <div className="ps-matrix-wrap">
        <table className="ps-matrix">
          <thead>
            <tr>
              <th scope="col" className="ps-matrix__corner">
                <span aria-hidden="true">
                  from ↓
                  <br />
                  to →
                </span>
                <span className="sr-only">from / to</span>
              </th>
              {states.map((to) => (
                <th key={to} scope="col">
                  <span className="ps-matrix__col">{to}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {states.map((from) => (
              <tr key={from}>
                <th scope="row">{from}</th>
                {states.map((to) => (
                  <td key={to}>
                    {from === to ? (
                      <span className="ps-cell--self" aria-hidden="true" />
                    ) : (
                      <button
                        type="button"
                        className="ps-cell"
                        aria-pressed={(transitions[from] || []).includes(to)}
                        aria-label={`${from} to ${to}`}
                        title={`${from} → ${to}`}
                        onClick={() => toggle(from, to)}
                      />
                    )}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="ps-matrix-legend">
        <span>
          <i aria-hidden="true" />
          allowed
        </span>
        <span>toggle a cell to allow or block it</span>
      </div>
    </>
  );
}
