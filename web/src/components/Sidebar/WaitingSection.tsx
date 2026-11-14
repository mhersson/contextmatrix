import { useNavigate } from 'react-router';
import { useAttention } from '../../hooks/useAttention';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { attentionHref } from '../../lib/attention';
import { formatRelativeTime } from '../../lib/format';
import type { AttentionItem } from '../../types';
import { BlueprintIcon, PlaybookIcon } from '../SessionIcons';
import { ShieldCheckIcon } from '../ShieldCheckIcon';
import { WaitBadge } from '../WaitBadge';

function CardIcon() {
  return (
    <svg width={14} height={14} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="3" width="16" height="18" rx="2" />
      <path d="M8 8h8M8 12h8M8 16h5" />
    </svg>
  );
}

function KindIcon({ kind }: { kind: AttentionItem['kind'] }) {
  switch (kind) {
    case 'blueprint':
      return <BlueprintIcon />;
    case 'playbook':
      return <PlaybookIcon />;
    case 'card':
      return <CardIcon />;
    case 'sentinel':
      return <ShieldCheckIcon size={14} />;
  }
}

/** Everything waiting for the viewer, above Work; absent while nothing waits. */
export function WaitingSection() {
  const { items } = useAttention();
  const navigate = useNavigate();
  const mobileSidebar = useMobileSidebar();
  if (items.length === 0) return null;

  const open = (item: AttentionItem) => {
    navigate(attentionHref(item));
    mobileSidebar.close();
  };

  return (
    <section className="sb-waiting" aria-label="Waiting for you">
      <div className="sb-eyebrow flex items-center gap-1.5 px-3 pt-1 pb-0.5">
        Waiting for you
        <WaitBadge count={items.length} />
      </div>
      <ul>
        {items.map((item) => (
          <li key={`${item.kind}:${item.project ?? ''}:${item.id}`}>
            <button type="button" className="sb-waiting__item" onClick={() => open(item)}>
              <span className="sb-waiting__icon">
                <KindIcon kind={item.kind} />
              </span>
              <span className="min-w-0">
                <span className="sb-waiting__title">
                  {(item.kind === 'card' || item.kind === 'sentinel') && <span className="font-mono">{item.id} </span>}
                  {item.title}
                  <span className="sb-waiting__who">
                    {!item.mine && 'shared · '}
                    {formatRelativeTime(item.since)}
                  </span>
                </span>
                <span className="sb-waiting__reason">
                  {item.reason}
                  {item.playbook && ` · ${item.playbook.title}`}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}
