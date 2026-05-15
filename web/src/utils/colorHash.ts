const ID_COLORS = [
  'var(--blue)',
  'var(--purple)',
  'var(--aqua)',
  'var(--orange)',
  'var(--yellow)',
];

export function idColor(id: string): string {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash * 31 + id.charCodeAt(i)) >>> 0;
  }
  return ID_COLORS[hash % ID_COLORS.length];
}
