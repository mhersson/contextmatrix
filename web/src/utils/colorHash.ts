const ID_COLORS = [
  'var(--blue)',
  'var(--purple)',
  'var(--aqua)',
  'var(--orange)',
  'var(--yellow)',
];

function hash(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) >>> 0;
  }
  return h;
}

export function idColor(id: string): string {
  return ID_COLORS[hash(id) % ID_COLORS.length];
}

/**
 * Returns two CSS variable references that form a stable, deterministic
 * gradient for an agent avatar. The pair is always distinct.
 */
export function avatarGradient(id: string): { from: string; to: string } {
  const h = hash(id);
  const from = ID_COLORS[h % ID_COLORS.length];
  // Skip ahead by half the palette so `to` is never equal to `from`.
  const to = ID_COLORS[(h + Math.floor(ID_COLORS.length / 2) + 1) % ID_COLORS.length];
  return { from, to };
}
