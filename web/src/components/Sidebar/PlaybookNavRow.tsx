import { NavLink } from 'react-router';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { formatAge, formatExactTime } from '../../lib/format';
import { playbookStatus } from '../../lib/playbookStatus';
import type { PlaybookSummary } from '../../types';
import { PinIcon } from '../SessionIcons';
import { WaitBadge } from '../WaitBadge';

const STATUS_DOT = {
  running: 'var(--aqua)',
  waiting: 'var(--wait-yellow)',
  stopped: 'var(--orange)',
  completed: 'var(--bg4)',
  'not started': 'var(--bg4)',
} as const;

export function PlaybookNavRow({ playbook: p, waiting, now }: { playbook: PlaybookSummary; waiting?: boolean; now: number }) {
  const mobileSidebar = useMobileSidebar();
  const status = playbookStatus(p);

  return (
    <li>
      <NavLink
        to={`/playbooks/${p.id}`}
        onClick={mobileSidebar.close}
        className={({ isActive }) =>
          `sb-navrow${isActive ? ' active' : ''} flex items-center gap-2 pl-3 pr-2 py-1.5 rounded text-[13.5px]`
        }
      >
        <span
          className="sb-dot"
          role="img"
          title={status}
          aria-label={status}
          style={{ backgroundColor: STATUS_DOT[status] }}
        />
        <span className="truncate flex-1">{p.title || p.id}</span>
        {waiting && <WaitBadge />}
        {p.pinned === true && (
          <span className="inline-flex shrink-0" style={{ color: 'var(--orange)' }}>
            <PinIcon size={12} filled label="Pinned" />
          </span>
        )}
        <span className="sb-age" title={formatExactTime(p.updated_at)}>
          {formatAge(p.updated_at, now)}
        </span>
      </NavLink>
    </li>
  );
}
