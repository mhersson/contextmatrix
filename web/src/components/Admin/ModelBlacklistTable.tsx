import type { ModelBlacklistEntry } from '../../types';
import { AdminTable, type AdminTableHeader } from './AdminTable';

interface ModelBlacklistTableProps {
  models: ModelBlacklistEntry[];
  loading: boolean;
  error: string | null;
  onDelist: (slug: string) => void;
}

function fmtSeen(ts: number): string {
  return ts > 0 ? new Date(ts * 1000).toLocaleString() : ' - ';
}

const HEADERS: AdminTableHeader[] = [
  { label: 'Model' },
  { label: 'Reason' },
  { label: 'Sample card' },
  { label: 'Reported by' },
  { label: 'Last seen' },
  { label: '' },
];

export function ModelBlacklistTable({ models, loading, error, onDelist }: ModelBlacklistTableProps) {
  return (
    <AdminTable
      loading={loading}
      error={error}
      empty={models.length === 0}
      emptyMessage="No models are blacklisted."
      headers={HEADERS}
    >
      {models.map((m) => (
        <tr key={m.slug} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
          <td className="px-4 py-2 font-mono">{m.slug}</td>
          <td className="px-4 py-2">{m.reason}</td>
          <td className="px-4 py-2 font-mono">{m.sample_card || ' - '}</td>
          <td className="px-4 py-2 font-mono">{m.reported_by}</td>
          <td className="px-4 py-2 whitespace-nowrap">{fmtSeen(m.last_seen)}</td>
          <td className="px-4 py-2 text-right">
            <button
              type="button"
              aria-label={`Delist ${m.slug}`}
              onClick={() => onDelist(m.slug)}
              className="rounded py-1 px-3 text-sm font-medium"
              style={{ backgroundColor: 'var(--bg3)', color: 'var(--fg)' }}
            >
              Delist
            </button>
          </td>
        </tr>
      ))}
    </AdminTable>
  );
}
