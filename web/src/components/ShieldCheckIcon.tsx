export function ShieldCheckIcon({ size = 13 }: { size?: number }) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 16 16">
      <path d="M8 1.2 2.2 3.3v4.2c0 3.4 2.4 6.1 5.8 7.4 3.4-1.3 5.8-4 5.8-7.4V3.3z" fill="currentColor" />
      <path
        d="M5.3 8.1l1.9 1.9 3.6-3.7"
        fill="none"
        style={{ stroke: 'var(--bg0)' }}
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
