import { useMobileSidebar } from '../../context/MobileSidebarContext';

interface Props {
  title: string;
  onNewChat: () => void;
}

export function MobileChatHeader({ title, onNewChat }: Props) {
  const { toggle } = useMobileSidebar();
  return (
    <header
      className="flex items-center justify-between gap-2 px-3 py-2 border-b md:hidden"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <button
        type="button"
        onClick={toggle}
        className="p-1 rounded hover:opacity-80 transition-opacity shrink-0"
        style={{ color: 'var(--fg)' }}
        aria-label="Toggle sidebar"
      >
        <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
          <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
          <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
        </svg>
      </button>
      <h1
        className="flex-1 min-w-0 truncate text-sm text-center font-normal m-0"
        style={{ color: 'var(--fg)' }}
        title={title}
      >
        {title}
      </h1>
      <button
        type="button"
        onClick={onNewChat}
        className="p-1 rounded hover:opacity-80 transition-opacity shrink-0"
        style={{ color: 'var(--aqua)' }}
        aria-label="New chat"
        title="New chat"
      >
        <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
        </svg>
      </button>
    </header>
  );
}
