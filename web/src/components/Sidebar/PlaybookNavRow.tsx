import { NavLink } from 'react-router';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { playbookStatus } from '../../lib/playbookStatus';
import type { PlaybookSummary } from '../../types';
import { PinIcon, PlaybookIcon } from '../SessionIcons';
import { WaitBadge } from '../WaitBadge';

const STATUS_TONE = {
  running: 'var(--aqua)',
  waiting: 'var(--yellow)',
  stopped: 'var(--red)',
  completed: 'var(--green)',
  'not started': 'var(--grey1)',
} as const;

export function PlaybookNavRow({ playbook: p, waiting }: { playbook: PlaybookSummary; waiting?: boolean }) {
  const mobileSidebar = useMobileSidebar();
  const status = playbookStatus(p);
  const progressed = status === 'running' || (status === 'not started' && p.complete > 0);
  const pill = progressed ? `${p.complete} of ${p.total}` : status;

  return (
    <li>
      <NavLink
        to={`/playbooks/${p.id}`}
        onClick={mobileSidebar.close}
        className={({ isActive }) =>
          `sb-navrow${isActive ? ' active' : ''} flex items-center gap-2 pl-3 pr-2 py-1.5 rounded text-[13.5px]`
        }
      >
        <span className="inline-flex shrink-0" style={{ color: 'var(--aqua)' }}>
          <PlaybookIcon />
        </span>
        <span className="truncate flex-1">{p.title || p.id}</span>
        {waiting && <WaitBadge />}
        {p.pinned === true && (
          <span className="inline-flex shrink-0" style={{ color: 'var(--orange)' }}>
            <PinIcon size={12} filled label="Pinned" />
          </span>
        )}
        <span className="shrink-0 font-mono text-[10.5px]" style={{ color: STATUS_TONE[status] }}>
          {pill}
        </span>
      </NavLink>
    </li>
  );
}
