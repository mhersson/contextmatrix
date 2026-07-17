import type { ChatSession } from '../../types';

interface ChatsTableProps {
  chats: ChatSession[];
  loading: boolean;
  error: string | null;
  onEnd: (chat: ChatSession) => void;
  onDelete: (chat: ChatSession) => void;
}

function fmtCost(v?: number): string {
  return v != null && v > 0 ? `$${v.toFixed(2)}` : ' - ';
}

function fmtWhen(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? ' - ' : d.toLocaleString();
}

/** Presentational table for the admin chats page. End is offered for any
 * non-cold session; Delete always. Deliberately renders no link into a
 * transcript - this surface is metadata + lifecycle only. */
export function ChatsTable({ chats, loading, error, onEnd, onDelete }: ChatsTableProps) {
  if (loading) {
    return (
      <div className="text-sm" style={{ color: 'var(--grey1)' }}>
        Loading…
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
        {error}
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="text-sm" style={{ color: 'var(--grey1)' }}>
        No chat sessions.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded border" style={{ borderColor: 'var(--bg3)' }}>
      <table className="w-full text-sm" style={{ color: 'var(--fg)' }}>
        <thead>
          <tr className="text-left" style={{ color: 'var(--grey2)', backgroundColor: 'var(--bg1)' }}>
            <th className="px-4 py-2 font-medium">Title</th>
            <th className="px-4 py-2 font-medium">Owner</th>
            <th className="px-4 py-2 font-medium">Project</th>
            <th className="px-4 py-2 font-medium">Status</th>
            <th className="px-4 py-2 font-medium">Cost</th>
            <th className="px-4 py-2 font-medium">Last active</th>
            <th className="px-4 py-2" />
          </tr>
        </thead>
        <tbody>
          {chats.map((c) => (
            <tr key={c.id} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
              <td className="px-4 py-2">{c.title || c.id}</td>
              <td className="px-4 py-2 font-mono text-xs">{c.created_by}</td>
              <td className="px-4 py-2">{c.project || ' - '}</td>
              <td className="px-4 py-2">{c.status}</td>
              <td className="px-4 py-2">{fmtCost(c.estimated_cost_usd)}</td>
              <td className="px-4 py-2">{fmtWhen(c.last_active)}</td>
              <td className="px-4 py-2 text-right whitespace-nowrap">
                {c.status !== 'cold' && (
                  <button
                    type="button"
                    onClick={() => onEnd(c)}
                    className="rounded px-2 py-1 text-xs mr-2"
                    style={{ backgroundColor: 'var(--bg-yellow)', color: 'var(--yellow)' }}
                  >
                    End
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => onDelete(c)}
                  className="rounded px-2 py-1 text-xs"
                  style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
