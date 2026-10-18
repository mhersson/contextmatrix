/** A drawing sheet with its title block. */
export function SheetIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.3} aria-hidden="true">
      <rect x="2.5" y="1.5" width="11" height="13" />
      <path d="M2.5 11h11M8 11v3.5" />
    </svg>
  );
}

/** Two overlapping speech bubbles: a question round's dialogue. */
export function DialogueIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.3} aria-hidden="true">
      <path d="M1.8 2.5h8v5.5H5.3L3 10.2V8H1.8z" />
      <path d="M11.3 5.5h2.9v5.6h-1.3v2.2l-2.3-2.2H6.8V9.6" />
    </svg>
  );
}

/** A magnifier over a reviewed version: a tick when it passed, an exclamation when it went back. */
export function ReviewIcon({ passed }: { passed: boolean }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.3} aria-hidden="true">
      <circle cx="7" cy="7" r="4.6" />
      <path d="M10.4 10.4l3.4 3.4" strokeLinecap="round" />
      {passed ? (
        <path d="M5 7.1l1.4 1.4 2.6-2.8" strokeLinecap="round" strokeLinejoin="round" />
      ) : (
        <path d="M7 4.8v2.7M7 9.2v.1" strokeLinecap="round" />
      )}
    </svg>
  );
}

/** A transcript card's open/closed chevron; the card's CSS turns it. */
export function ChevronIcon({ className }: { className: string }) {
  return (
    <svg className={className} width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth={1.4} aria-hidden="true">
      <path d="M3.5 1.5 7 5 3.5 8.5" />
    </svg>
  );
}
