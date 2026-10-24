import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { SidebarMenuButton } from './SidebarMenuButton';

/** Slim mobile-only bar giving a main-area page the same hamburger
 *  affordance as the dashboard's CommandStrip - just sidebar access. */
export function PageBar({ title }: { title: string }) {
  const { toggle } = useMobileSidebar();

  return (
    <div className="md:hidden flex items-center gap-2 px-4 py-2 border-b" style={{ borderColor: 'var(--bg3)' }}>
      <SidebarMenuButton onClick={toggle} />
      <span className="truncate" style={{ color: 'var(--fg)' }}>{title}</span>
    </div>
  );
}
