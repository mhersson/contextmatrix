import type { ModelOutcomeEntry } from '../../types';
import { AdminTable, type AdminTableHeader } from './AdminTable';

interface ModelOutcomesTableProps {
  models: ModelOutcomeEntry[];
  loading: boolean;
  error: string | null;
}

function fmtCost(v: number): string {
  return v > 0 ? `$${v.toFixed(2)}` : ' - ';
}

const HEADERS: AdminTableHeader[] = [
  { label: 'Model' },
  { label: 'Races' },
  { label: 'Race wins' },
  { label: 'Race win rate' },
  { label: 'Solo runs' },
  { label: 'Solo failures' },
  { label: 'Cost' },
];

export function ModelOutcomesTable({ models, loading, error }: ModelOutcomesTableProps) {
  return (
    <AdminTable
      loading={loading}
      error={error}
      empty={models.length === 0}
      emptyMessage="No model outcomes recorded yet."
      headers={HEADERS}
    >
      {models.map((m) => (
        <tr key={m.model} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
          <td className="px-4 py-2 font-mono">{m.model}</td>
          <td className="px-4 py-2">{m.race_samples}</td>
          <td className="px-4 py-2">{m.race_wins}</td>
          <td className="px-4 py-2">
            {m.race_samples > 0 ? `${Math.round(m.race_win_rate * 100)}%` : ' - '}
          </td>
          <td className="px-4 py-2">{m.solo_samples}</td>
          <td className="px-4 py-2">{m.solo_failures}</td>
          <td className="px-4 py-2">{fmtCost(m.total_cost_usd)}</td>
        </tr>
      ))}
    </AdminTable>
  );
}
