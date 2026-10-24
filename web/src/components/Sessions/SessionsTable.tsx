import type { ChatSession } from '../../types';
import { SessionRow } from './SessionRow';

interface SessionsTableProps {
  rows: ChatSession[];
  /** The Blueprints tab adds the Mode column. */
  showMode: boolean;
  selected: ReadonlySet<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  onPin: (session: ChatSession) => void;
  onOpen: (id: string) => void;
}

export function SessionsTable({ rows, showMode, selected, onToggle, onToggleAll, onPin, onOpen }: SessionsTableProps) {
  const ticked = rows.filter((s) => selected.has(s.id)).length;
  const all = rows.length > 0 && ticked === rows.length;

  return (
    <div className="ssn-table-wrap">
      <table className="ssn-table">
        <thead>
          <tr>
            <th className="ssn-col-tick">
              <input
                type="checkbox"
                className="ssn-tick"
                aria-label="Select all shown"
                checked={all}
                ref={(el) => {
                  if (el) el.indeterminate = ticked > 0 && !all;
                }}
                onChange={onToggleAll}
              />
            </th>
            <th className="ssn-col-pin"><span className="sr-only">Pinned</span></th>
            <th>Title</th>
            <th>{showMode ? 'Projects' : 'Project'}</th>
            {showMode && <th>Mode</th>}
            <th>Cost</th>
            <th>Last active</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              showMode={showMode}
              ticked={selected.has(s.id)}
              onToggle={onToggle}
              onPin={onPin}
              onOpen={onOpen}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}
