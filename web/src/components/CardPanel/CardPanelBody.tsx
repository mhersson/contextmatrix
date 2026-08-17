import type { ReactNode } from 'react';
import { useMediaQuery } from '../../hooks/useMediaQuery';

export type RailTabKey = 'card' | 'chat' | 'automation' | 'info' | 'danger';

/** Rail width levels. All three reshape the split inside a drawer of constant
 *  width; `full` is the degenerate case where the left column drops out and
 *  the rail gets the whole panel. */
export type RailMode = 'collapsed' | 'expanded' | 'full';

export interface RailTab {
  key: RailTabKey;
  label: string;
  indicator?: ReactNode;
  content: ReactNode;
}

interface CardPanelBodyProps {
  left: ReactNode;
  tabs: RailTab[];
  activeTab: RailTabKey;
  onTabChange: (tab: RailTabKey) => void;
  railMode: RailMode;
  onToggleRail: () => void;
  /** Omitted by surfaces that don't offer full width (card creation), which
   *  drops the toggle rather than exposing a mode they can't enter. */
  onToggleFull?: () => void;
}

/**
 * Bifold body: left column (labels + description) + right rail (tabs).
 *
 * Collapsed rail → grid 1fr / minmax(320px, 380px).
 * Expanded rail → grid 40% / 60% (the orchestrator widens the whole panel).
 * Full rail → single column; the left column drops out and the rail spans the
 * drawer, whose own width is unchanged.
 *
 * The rail strip sits above the tab content and contains the tab buttons plus
 * the width controls on the right: an Expand/Collapse chevron and a full-width
 * toggle. Only the full-width toggle survives in full mode - the chevron
 * reshapes a split that no longer exists there.
 *
 * On narrow viewports (≤ 768px) the two columns collapse into a single
 * rail-only column. The left-column content is injected as the first tab
 * ("Card") so Labels + Description remain reachable without the horizontal
 * split. Both width controls are hidden in this mode (meaningless with one
 * column). Full mode does NOT inject that tab: it is an explicit, temporary
 * choice with the toggle that undoes it one click away.
 */
export function CardPanelBody({
  left,
  tabs,
  activeTab,
  onTabChange,
  railMode,
  onToggleRail,
  onToggleFull,
}: CardPanelBodyProps) {
  const isMobile = useMediaQuery('(max-width: 768px)');
  const isFull = railMode === 'full';
  const singleColumn = isMobile || isFull;

  const renderedTabs: RailTab[] = isMobile
    ? [
        {
          key: 'card',
          label: 'Card',
          content: (
            <div className="overflow-y-auto overflow-x-hidden p-5 space-y-5 min-w-0 flex-1">
              {left}
            </div>
          ),
        },
        ...tabs,
      ]
    : tabs;

  const active = renderedTabs.find((t) => t.key === activeTab) ?? renderedTabs[0];

  // The panel itself is a fixed width (see `.card-panel-bifold` CSS).
  // `Expand rail` reshapes the internal split: the rail grows from the
  // collapsed width to the expanded width and the left column shrinks by
  // the same amount. Widths come from CSS custom properties defined in
  // `index.css` so themes/breakpoints can override without touching JSX.
  const gridTemplate = singleColumn
    ? '1fr'
    : railMode === 'expanded'
      ? '1fr var(--rail-expanded-width, 600px)'
      : '1fr var(--rail-collapsed-width, 340px)';

  return (
    <div
      className="flex-1 min-h-0 grid"
      data-testid="body-bifold"
      style={{ gridTemplateColumns: gridTemplate }}
    >
      {!singleColumn && (
        <div
          className="overflow-y-auto overflow-x-hidden p-5 space-y-5 border-r border-[var(--bg3)] min-w-0"
          data-testid="body-left"
        >
          {left}
        </div>
      )}

      {/* Right rail */}
      <div className="flex flex-col min-w-0 min-h-0" data-testid="body-rail">
        {/* Tab strip */}
        <div
          className="flex items-stretch border-b border-[var(--bg3)] bg-[var(--bg0)] sticky top-0 z-10"
          role="tablist"
          aria-label="Card detail tabs"
        >
          {renderedTabs.map((t) => {
            const isActive = t.key === active.key;
            const isDanger = t.key === 'danger';
            return (
              <button
                key={t.key}
                id={`rail-tab-${t.key}`}
                role="tab"
                type="button"
                aria-selected={isActive}
                aria-controls={`rail-panel-${t.key}`}
                onClick={() => onTabChange(t.key)}
                className={`bf-rail-tab${isActive ? ' bf-rail-tab--active' : ''}${isDanger ? ' bf-rail-tab--danger' : ''}`}
              >
                {t.indicator}
                <span>{t.label}</span>
              </button>
            );
          })}

          {!isMobile && (
            <div className="bf-rail-controls">
              {!isFull && (
                <button
                  type="button"
                  onClick={onToggleRail}
                  className="bf-rail-expand"
                  aria-label={railMode === 'expanded' ? 'Collapse rail' : 'Expand rail'}
                  aria-pressed={railMode === 'expanded'}
                  title={railMode === 'expanded' ? 'Collapse rail' : 'Expand rail'}
                >
                  <span className="bf-rail-expand-arrow">
                    {railMode === 'expanded' ? '›‹' : '‹›'}
                  </span>
                </button>
              )}

              {/* Labelled only on the way out: the collapsed strip has no room
                  for text, but full mode has the whole drawer and the exit
                  should name itself rather than leave the user reading a
                  glyph. */}
              {onToggleFull && (
                <button
                  type="button"
                  onClick={onToggleFull}
                  className="bf-rail-expand"
                  aria-label={isFull ? 'Exit full width' : 'Full width'}
                  aria-pressed={isFull}
                  title={isFull ? 'Exit full width' : 'Full width'}
                >
                  <span className="bf-rail-expand-arrow" aria-hidden="true">
                    {isFull ? '⤡' : '⤢'}
                  </span>
                  {isFull && <span>Exit full width</span>}
                </button>
              )}
            </div>
          )}
        </div>

        {/* Active tab content */}
        <div
          id={`rail-panel-${active.key}`}
          role="tabpanel"
          aria-labelledby={`rail-tab-${active.key}`}
          className="flex-1 min-h-0 flex flex-col"
          data-testid={`rail-panel-${active.key}`}
        >
          {active.content}
        </div>
      </div>
    </div>
  );
}
