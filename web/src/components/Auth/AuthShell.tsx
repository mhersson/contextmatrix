import { type CSSProperties, type ReactNode } from 'react';
import { useAuth } from '../../hooks/useAuth';
import { BoardVignette } from './BoardVignette';
import { MatrixGlyph } from './MatrixGlyph';

interface AuthShellProps {
  /** Optional helper line rendered between the card and the meta line. */
  hint?: string;
  children: ReactNode;
}

/**
 * Split-screen chrome for the auth pages: brand panel (wordmark, tagline,
 * board vignette) on the left, the form card on --bg-dim on the right.
 * The brand panel collapses below md; a compact wordmark appears above the
 * card instead.
 */
export function AuthShell({ hint, children }: AuthShellProps) {
  const { mode, version } = useAuth();
  // Tag-like versions get a "v" prefix; dev build stamps ("2026-07-04 13:16
  // 0a8e739") are shown as-is and truncated by the meta line's ellipsis.
  const versionLabel = version ? (version.includes(' ') ? version : `v${version}`) : null;
  const meta = [mode === 'multi' ? 'multi-user mode' : null, versionLabel].filter(Boolean).join(' · ');

  return (
    <div className="h-screen flex overflow-hidden" style={{ backgroundColor: 'var(--bg-dim)' }}>
      <aside className="auth-brand hidden md:flex">
        <BrandCells />
        <div className="auth-rise flex flex-col gap-4">
          <Wordmark glyphSize={28} nameClass="text-3xl" />
          <p className="text-[13.5px] leading-relaxed max-w-[300px]" style={{ color: 'var(--grey1)' }}>
            Task coordination for AI agents and humans.
          </p>
        </div>
        <BoardVignette />
        <div className="auth-brandfoot" aria-hidden="true">
          <span className="auth-commitdot" />
          cards are markdown files · every change is a git commit
        </div>
      </aside>

      <main className="relative flex-1 flex flex-col items-center justify-center overflow-y-auto px-6 py-10">
        <PanelGlow />
        <div className="md:hidden mb-8 auth-rise relative">
          <Wordmark glyphSize={22} nameClass="text-xl" />
        </div>
        <div className="relative w-full max-w-sm">
          <div className="auth-card auth-rise-card">{children}</div>
          {hint && (
            <p className="auth-rise-meta mt-4 text-center text-xs" style={{ color: 'var(--grey1)' }}>
              {hint}
            </p>
          )}
          {meta && (
            <div className={`auth-meta auth-rise-meta ${hint ? 'mt-2' : 'mt-4'}`}>
              <span className="auth-live" aria-hidden="true" />
              <span className="truncate" title={meta}>
                {meta}
              </span>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

// Cell-grid coordinates (33px units), scattered clear of the wordmark and
// vignette. Rows beyond the viewport are simply clipped on short windows.
const LIT_CELLS: { c: number; r: number; color: string; breathe?: boolean }[] = [
  { c: 14, r: 2, color: '--purple' },
  { c: 16, r: 6, color: '--aqua' },
  { c: 3, r: 13, color: '--green', breathe: true },
  { c: 12, r: 15, color: '--yellow' },
  { c: 16, r: 19, color: '--blue' },
  { c: 5, r: 22, color: '--aqua', breathe: true },
  { c: 11, r: 26, color: '--green' },
  { c: 2, r: 30, color: '--purple' },
  { c: 15, r: 34, color: '--yellow' },
];

/** Soft ambient glow behind the form card. The inset clipping layer keeps
 *  the oversized blurred blobs from extending the panel's scroll area. */
function PanelGlow() {
  return (
    <div className="auth-glow" aria-hidden="true">
      <span className="auth-glow-a" />
      <span className="auth-glow-b" />
    </div>
  );
}

/** Full-bleed matrix backdrop: hairline cell grid with a few lit cells. */
function BrandCells() {
  return (
    <div className="auth-brand-cells" aria-hidden="true">
      {LIT_CELLS.map(({ c, r, color, breathe }) => (
        <span
          key={`${c}-${r}`}
          className={breathe ? 'auth-cell auth-cell--breathe' : 'auth-cell'}
          style={{ left: c * 33 + 1, top: r * 33 + 1, '--cell-c': `var(${color})` } as CSSProperties}
        />
      ))}
    </div>
  );
}

function Wordmark({ glyphSize, nameClass }: { glyphSize: number; nameClass: string }) {
  return (
    <div className="flex items-center gap-3">
      <MatrixGlyph size={glyphSize} />
      <span className={`${nameClass} font-medium`} style={{ fontFamily: 'var(--font-display)', color: 'var(--fg)' }}>
        ContextMatrix
      </span>
    </div>
  );
}
