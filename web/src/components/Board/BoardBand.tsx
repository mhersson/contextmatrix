import type { ReactNode, Ref } from 'react';
import { HeaderCollapseToggle } from './HeaderCollapseToggle';
import { SidebarMenuButton } from '../SidebarMenuButton/SidebarMenuButton';

interface BoardBandProps {
  projectName: string;
  displayName?: string;
  activeAgents: number;
  openCount: number;
  inReviewCount: number;
  shippedToday: number;
  onCreateCard: () => void;
  onToggleCollapsed?: () => void;
  toggleRef?: Ref<HTMLButtonElement>;
  shippedLast7d?: number;
  shippedPrior7d?: number;
  /** Secondary actions rendered immediately left of New Card. */
  actions?: ReactNode;
  /** Mobile-only sidebar opener, rendered at the crumb row's left edge. */
  onOpenSidebar?: () => void;
}

/**
 * Header band for the project board. Editorial-engineering style: mono
 * crumb, Fraunces hero title, sub-line with rolling stats, +New Card
 * primary action. Hairline aqua tick fades under the title.
 *
 * Two rows: the crumb row carries the collapse toggle at its right edge,
 * the main row carries title + sub-line with the action cluster + New Card
 * bottom-right, so the toggle lines up with the crumb and shares New Card's
 * right edge. The band is the board's only header - there is no bar above it.
 *
 * Subheader stats count delivery units only (cards where `!parent`);
 * subtasks are excluded so decomposition does not inflate the rolling
 * headline. The caller passes parent-only counts.
 */
export function BoardBand({
  projectName,
  displayName,
  activeAgents,
  openCount,
  inReviewCount,
  shippedToday,
  onCreateCard,
  onToggleCollapsed,
  toggleRef,
  shippedLast7d,
  shippedPrior7d,
  actions,
  onOpenSidebar,
}: BoardBandProps) {
  const title = displayName ?? projectName;

  const showDelta = shippedLast7d !== undefined && shippedPrior7d !== undefined && shippedPrior7d > 0;
  const deltaPct = showDelta
    ? Math.round(((shippedLast7d - shippedPrior7d) / shippedPrior7d) * 100)
    : 0;
  const deltaUp = showDelta && shippedLast7d >= shippedPrior7d;

  return (
    <div className="board-band">
      <div className="board-band__top">
        <div className="board-band__top-left">
          {onOpenSidebar && <SidebarMenuButton onClick={onOpenSidebar} />}
          <div className="board-band__crumb">
            <span>Projects</span>
            <span className="dot" />
            <span className="accent">{projectName}</span>
          </div>
        </div>
        {onToggleCollapsed && <HeaderCollapseToggle ref={toggleRef} collapsed={false} onToggle={onToggleCollapsed} />}
      </div>
      <div className="board-band__main">
        <div>
          <h2 className="board-band__title">{title}</h2>
          <div className="board-band__sub">
            <span className={activeAgents > 0 ? 'board-band__pulse' : undefined}>{activeAgents} agents live</span>
            <span className="board-band__sep">·</span>
            <span>{openCount} open · {inReviewCount} in review · {shippedToday} shipped today</span>
            {shippedLast7d !== undefined && (
              <>
                <span className="board-band__sep">·</span>
                <span>
                  {shippedLast7d} shipped this week
                  {showDelta && (
                    <>
                      {' '}·{' '}
                      <span style={{ color: deltaUp ? 'var(--green)' : 'var(--red)' }}>
                        {deltaUp ? '+' : ''}
                        {deltaPct}%
                      </span>
                    </>
                  )}
                </span>
              </>
            )}
          </div>
        </div>
        <div className="board-band__actions">
          {actions}
          <button
            type="button"
            onClick={onCreateCard}
            className="board-band__cta px-3 py-2 rounded font-medium bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90 transition-opacity inline-flex items-center gap-2 flex-shrink-0 whitespace-nowrap"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2.5} viewBox="0 0 24 24"><path d="M12 5v14M5 12h14" strokeLinecap="round" strokeLinejoin="round" /></svg>
            New Card
          </button>
        </div>
      </div>
    </div>
  );
}
