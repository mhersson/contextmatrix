interface MatrixGlyphProps {
  size?: number;
}

/** 3×3 kanban-cell brand glyph: three cells lit in aqua/green/purple. */
export function MatrixGlyph({ size = 24 }: MatrixGlyphProps) {
  const u = size / 22;
  const cell = (col: number, row: number, fill?: string) => (
    <rect
      key={`${col}-${row}`}
      x={col * 8 * u}
      y={row * 8 * u}
      width={6 * u}
      height={6 * u}
      rx={1.7 * u}
      fill={fill ?? 'var(--bg4)'}
      opacity={fill ? 1 : 0.5}
    />
  );

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
      {cell(0, 0)}
      {cell(1, 0, 'var(--aqua)')}
      {cell(2, 0)}
      {cell(0, 1, 'var(--green)')}
      {cell(1, 1)}
      {cell(2, 1)}
      {cell(0, 2)}
      {cell(1, 2)}
      {cell(2, 2, 'var(--purple)')}
    </svg>
  );
}
