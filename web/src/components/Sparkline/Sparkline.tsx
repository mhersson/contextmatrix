export function Sparkline({ values, color }: { values?: number[]; color: string }) {
  if (!values || values.length < 2) return null;
  const max = Math.max(...values, 1);
  const w = 80;
  const h = 22;
  const step = w / (values.length - 1);
  const points = values
    .map((v, i) => {
      // Clamp so a constant-zero series sits on the baseline rather than the top.
      const y = max === 0 ? h : h - (v / max) * h;
      return `${(i * step).toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <polyline points={points} fill="none" stroke={color} strokeWidth="1.5" />
    </svg>
  );
}
