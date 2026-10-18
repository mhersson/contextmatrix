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
