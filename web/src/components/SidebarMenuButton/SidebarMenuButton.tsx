/**
 * Mobile-only hamburger that opens the sidebar drawer. Every page header owns
 * one (UtilityBar, PlaybooksBar, MobileChatHeader, the board bands,
 * ProjectCrumb) so the drawer is reachable from every route below the md
 * breakpoint.
 */
export function SidebarMenuButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Open menu"
      className={`md:hidden shrink-0 inline-flex items-center justify-center p-1.5 border-0 bg-transparent cursor-pointer rounded transition-[opacity,background-color] hover:opacity-90 hover:bg-[var(--bg1)] focus-visible:ring-2 focus-visible:ring-[var(--aqua)] focus-visible:outline-none`}
      style={{ color: 'var(--grey2)' }}
    >
      <svg width={20} height={20} viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
        <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
        <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
        <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
      </svg>
    </button>
  );
}
