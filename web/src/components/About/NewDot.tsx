export function NewDot({ label }: { label?: string }) {
  return (
    <>
      <span
        data-testid="new-dot"
        aria-hidden="true"
        className="inline-block h-[7px] w-[7px] shrink-0 rounded-full"
        style={{ backgroundColor: 'var(--green)' }}
      />
      {label && <span className="sr-only">{label}</span>}
    </>
  );
}
