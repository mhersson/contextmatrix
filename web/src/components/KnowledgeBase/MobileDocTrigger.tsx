interface MobileDocTriggerProps {
  docName?: string;
  onClick: () => void;
}

export function MobileDocTrigger({ docName, onClick }: MobileDocTriggerProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Open document selector"
      className="md:hidden px-4 py-2 flex items-center gap-2 text-sm w-full text-left"
      style={{ borderBottom: '1px solid var(--bg3)', color: 'var(--fg)', backgroundColor: 'var(--bg0)' }}
    >
      <svg
        className="w-4 h-4 flex-shrink-0"
        style={{ color: 'var(--grey1)' }}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
        <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
      </svg>
      {docName ? (
        <span className="flex-1 truncate" aria-hidden="true">
          {docName}
        </span>
      ) : (
        <span className="flex-1" style={{ color: 'var(--grey1)' }}>
          Choose a document
        </span>
      )}
      <svg
        className="w-4 h-4 flex-shrink-0"
        style={{ color: 'var(--grey1)' }}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <polyline points="9 18 15 12 9 6" />
      </svg>
    </button>
  );
}
