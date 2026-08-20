import { Link } from 'react-router';
import type { PlaybookSummary } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';
import { SegmentedProgress } from './SegmentedProgress';

interface PlaybookRowProps {
  playbook: PlaybookSummary;
}

export function PlaybookRow({ playbook: p }: PlaybookRowProps) {
  return (
    <Link
      to={`/playbooks/${p.id}`}
      className="block rounded-[10px] border mb-2 px-4 py-3 transition-colors"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg2)' }}
    >
      <div className="flex items-center gap-4">
        <span className="flex-1 font-medium" style={{ color: 'var(--fg)' }}>{p.title}</span>
        <span className="font-mono text-xs" style={{ color: 'var(--grey1)' }}>{p.complete}/{p.total}</span>
        <SegmentedProgress segments={p.segments} className="w-28" />
        <span className="text-xs" style={{ color: 'var(--grey0)' }}>{formatRelativeTime(p.updated_at)}</span>
      </div>
      <div className="text-xs mt-1" style={{ color: 'var(--grey1)' }}>
        {p.total} entries{p.projects > 0 ? ` · ${p.projects} project${p.projects === 1 ? '' : 's'}` : ''}
      </div>
    </Link>
  );
}
