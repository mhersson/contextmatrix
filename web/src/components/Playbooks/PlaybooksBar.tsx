import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { SidebarMenuButton } from '../SidebarMenuButton/SidebarMenuButton';

/** Slim mobile-only bar giving the Playbooks page the same hamburger
 *  affordance as the dashboard's CommandStrip - just sidebar access. */
export function PlaybooksBar() {
  const { toggle } = useMobileSidebar();

  return (
    <div className="md:hidden flex items-center gap-2 px-4 py-2 border-b" style={{ borderColor: 'var(--bg3)' }}>
      <SidebarMenuButton onClick={toggle} />
      <span className="truncate" style={{ color: 'var(--fg)' }}>Playbooks</span>
    </div>
  );
}
