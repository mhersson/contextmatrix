import { chipTint } from '../../lib/chip';
import type { ModelOutcomeEntry } from '../../types';

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

/**
 * Loading/error/empty-state wrapper and `<table>` markup for the Model
 * selection data page. Purely presentational - AdminModelSelectionPage owns
 * all data fetching and the reset action; this component only renders the
 * current stats.
 */
export function ModelOutcomesTable({ models, loading, error }: ModelOutcomesTableProps) {
  return (
    <div
      className="rounded-lg border overflow-hidden"
      style={{ backgroundColor: 'var(--bg1)', borderColor: 'var(--bg3)' }}
    >
      {loading ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey1)' }}>
          Loading…
        </div>
      ) : error ? (
        <div className="p-6 text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {error}
        </div>
      ) : models.length === 0 ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey0)' }}>
          No model outcomes recorded yet.
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="w-full text-sm" style={{ color: 'var(--fg)' }}>
            <thead>
              <tr style={{ color: 'var(--grey2)' }}>
                <th className="text-left px-4 py-2 font-medium">Model</th>
                <th className="text-left px-4 py-2 font-medium">Samples</th>
                <th className="text-left px-4 py-2 font-medium">Wins</th>
                <th className="text-left px-4 py-2 font-medium">Win rate</th>
                <th className="text-left px-4 py-2 font-medium">Cost</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
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
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
