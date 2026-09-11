import type { CSSProperties } from 'react';
import type { ModelBlacklistEntry } from '../../types';

interface ModelBlacklistTableProps {
  models: ModelBlacklistEntry[];
  loading: boolean;
  error: string | null;
  onDelist: (slug: string) => void;
}

function fmtSeen(ts: number): string {
  return ts > 0 ? new Date(ts * 1000).toLocaleString() : ' - ';
}

export function ModelBlacklistTable({ models, loading, error, onDelist }: ModelBlacklistTableProps) {
  const meta = loading ? 'loading' : models.length === 0 ? 'none blacklisted' : `${models.length} blacklisted · excluded from picks and seats`;

  return (
    <section className="apd-panel" style={{ '--apd-acc': 'var(--red)' } as CSSProperties}>
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">Blacklisted models</h2>
        <span className="apd-panel-meta">{meta}</span>
      </div>
      <div className="apd-panel-body">
        {loading ? (
          <div className="apd-panel-empty">Loading…</div>
        ) : error ? (
          <div className="apd-panel-empty tl-error" role="alert">
            {error}
          </div>
        ) : models.length === 0 ? (
          <div className="apd-panel-empty">No models are blacklisted.</div>
        ) : (
          <div className="tl-bl-scroll">
            <table className="tl-bl-table">
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Reason</th>
                  <th>Sample card</th>
                  <th>Reported by</th>
                  <th>First seen</th>
                  <th>Last seen</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {models.map((m) => (
                  <tr key={m.slug}>
                    <td>
                      <span className="tl-bl-slug">
                        <span className="chip-pill">blacklisted</span>
                        <span className="tl-bl-mono tl-bl-slug-text">{m.slug}</span>
                      </span>
                    </td>
                    <td className="tl-bl-reason">{m.reason}</td>
                    <td className="tl-bl-mono">{m.sample_card || ' - '}</td>
                    <td className="tl-bl-mono">{m.reported_by}</td>
                    <td className="tl-bl-mono">{fmtSeen(m.first_seen)}</td>
                    <td className="tl-bl-mono">{fmtSeen(m.last_seen)}</td>
                    <td className="tl-bl-actions">
                      <button type="button" className="bf-btn-danger bf-btn-sm" aria-label={`Delist ${m.slug}`} onClick={() => onDelist(m.slug)}>
                        Delist
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <p className="tl-foot">
        Models the agent reported incapable of driving the tool loop. A blacklisted model is excluded from every automatic pick and every panel
        seat above; only a card pin overrides it. Delisting makes it selectable again on the next run.
      </p>
    </section>
  );
}
