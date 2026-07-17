import { chipTint } from '../../lib/chip';
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

function fmtPercent(v: number): string {
  return `${Math.round(v * 100)}%`;
}

const HEADERS: AdminTableHeader[] = [
  { label: 'Model' },
  { label: 'Samples' },
  { label: 'Wins' },
  { label: 'Win rate' },
  { label: 'Cost' },
  { label: 'Status' },
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
          <td className="px-4 py-2">{m.samples}</td>
          <td className="px-4 py-2">{m.wins}</td>
          <td className="px-4 py-2">{fmtPercent(m.win_rate)}</td>
          <td className="px-4 py-2">{fmtCost(m.total_cost_usd)}</td>
          <td className="px-4 py-2">
            <span className="chip-pill" style={chipTint(m.active ? 'var(--green)' : 'var(--grey1)')}>
              {m.active ? 'Active' : 'Inert'}
            </span>
          </td>
        </tr>
      ))}
    </AdminTable>
  );
}
