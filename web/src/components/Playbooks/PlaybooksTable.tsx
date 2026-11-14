import type { ReactNode } from 'react';
import type { AttentionItem, PlaybookSummary } from '../../types';
import { PlaybookTableRow } from './PlaybookTableRow';

interface PlaybooksTableProps {
  rows: PlaybookSummary[];
  /** The create row, drawn above the playbooks. */
  createRow?: ReactNode;
  selected: ReadonlySet<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  onPin: (playbook: PlaybookSummary) => void;
  onOpen: (id: string) => void;
  waiting: ReadonlyMap<string, AttentionItem>;
}

export function PlaybooksTable({ rows, createRow, selected, onToggle, onToggleAll, onPin, onOpen, waiting }: PlaybooksTableProps) {
  const ticked = rows.filter((p) => selected.has(p.id)).length;
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
            <th className="ssn-col-title">Title</th>
            <th className="ssn-col-projects">Projects</th>
            <th>Run</th>
            <th>Progress</th>
            <th>Last active</th>
          </tr>
        </thead>
        <tbody>
          {createRow}
          {rows.map((p) => (
            <PlaybookTableRow
              key={p.id}
              playbook={p}
              ticked={selected.has(p.id)}
              onToggle={onToggle}
              onPin={onPin}
              onOpen={onOpen}
              waiting={waiting.get(p.id)}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}
