import type { ReactNode } from 'react';

export interface AdminTableHeader {
  label: string;
  align?: 'left' | 'right';
}

interface AdminTableProps {
  loading: boolean;
  error: string | null;
  empty: boolean;
  emptyMessage: string;
  headers: AdminTableHeader[];
  children: ReactNode;
}

/**
 * Container, loading/error/empty states, and table/thead chrome shared by
 * the admin tables; children are the `<tbody>` rows. ChatsTable deliberately
 * diverged from this chrome and stays standalone.
 */
export function AdminTable({ loading, error, empty, emptyMessage, headers, children }: AdminTableProps) {
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
      ) : empty ? (
        <div className="p-6 text-sm" style={{ color: 'var(--grey0)' }}>
          {emptyMessage}
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="w-full text-sm" style={{ color: 'var(--fg)' }}>
            <thead>
              <tr style={{ color: 'var(--grey2)' }}>
                {headers.map((h) => (
                  <th
                    key={h.label}
                    className={`${h.align === 'right' ? 'text-right' : 'text-left'} px-4 py-2 font-medium`}
                  >
                    {h.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>{children}</tbody>
          </table>
        </div>
      )}
    </div>
  );
}
