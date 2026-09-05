import { Link } from 'react-router';
import type { PlaybookSummary } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { RouteTrack } from './RouteTrack';

interface PlaybookRowProps {
  playbook: PlaybookSummary;
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function PlaybookRow({ playbook: p }: PlaybookRowProps) {
  const agentActive = p.segments.includes('active');
  const missing = p.segments.includes('missing');
  const meta = p.projects > 0
    ? `${count(p.total, 'entry', 'entries')} across ${count(p.projects, 'project', 'projects')}`
    : count(p.total, 'entry', 'entries');

  return (
    <Link to={`/playbooks/${p.id}`} className="pbl-row">
      <div className="pbl-row-head">
        <span className="pbl-row-title">{p.title}</span>
        <span className="pbl-row-frac" aria-hidden="true"><b>{p.complete}</b> of {p.total}</span>
        <span className="pbl-row-time">{formatRelativeTime(p.updated_at)}</span>
      </div>

      <RouteTrack segments={p.segments} gates={p.gates} />

      <div className="pbl-row-meta">
        <span>{meta}</span>
        {agentActive && <span className="pbl-pill pbl-pill-active">agent active</span>}
        {missing && <span className="pbl-pill pbl-pill-missing">missing card</span>}
      </div>

      {p.next && (
        <div className="pbl-row-next">
          <span className="pb-nextup-chip">next up</span>
          {p.next.type === 'card' && <span className="pbl-next-id">{p.next.card}</span>}
          <span className="pbl-next-title">
            {p.next.type === 'card' ? p.next.title || '(unknown card)' : p.next.title}
          </span>
        </div>
      )}
    </Link>
  );
}

export function PlaybookReceipt({ playbook: p }: PlaybookRowProps) {
  return (
    <Link to={`/playbooks/${p.id}`} className="pbl-receipt">
      <span className="pbl-receipt-tick" aria-hidden="true">✓</span>
      <span className="pbl-receipt-title">{p.title}</span>
      <span className="pbl-receipt-meta">{count(p.total, 'entry', 'entries')}</span>
      <span className="pbl-receipt-meta">{formatRelativeTime(p.updated_at)}</span>
    </Link>
  );
}
