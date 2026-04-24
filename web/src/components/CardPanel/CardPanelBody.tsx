import type { ReactNode } from 'react';

export type RailTabKey = 'chat' | 'automation' | 'info' | 'danger';

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
  railExpanded: boolean;
  onToggleRail: () => void;
}

/**
 * Bifold body: left column (labels + description) + right rail (tabs).
 *
 * Collapsed rail → grid 1fr / minmax(320px, 380px).
 * Expanded rail → grid 40% / 60% (the orchestrator widens the whole panel).
 *
 * The rail strip sits above the tab content and contains the tab buttons plus
 * a permanent Expand/Collapse toggle on the right.
 */
export function CardPanelBody({
  left,
  tabs,
  activeTab,
  onTabChange,
  railExpanded,
  onToggleRail,
}: CardPanelBodyProps) {
  const active = tabs.find((t) => t.key === activeTab) ?? tabs[0];

  // The panel itself is a fixed width (see `.card-panel-bifold` CSS).
  // `Expand rail` reshapes the internal split: the rail grows from the
  // collapsed width to the expanded width and the left column shrinks by
  // the same amount. Widths come from CSS custom properties defined in
  // `index.css` so themes/breakpoints can override without touching JSX.
  const gridTemplate = railExpanded
    ? '1fr var(--rail-expanded-width, 600px)'
    : '1fr var(--rail-collapsed-width, 340px)';

  return (
    <div
      className="flex-1 min-h-0 grid"
      data-testid="body-bifold"
      style={{ gridTemplateColumns: gridTemplate }}
    >
      {/* Left column */}
      <div
        className="overflow-y-auto overflow-x-hidden p-5 space-y-5 border-r border-[var(--bg3)] min-w-0"
        data-testid="body-left"
      >
        {left}
      </div>

      {/* Right rail */}
      <div className="flex flex-col min-w-0 min-h-0" data-testid="body-rail">
        {/* Tab strip */}
        <div
          className="flex items-stretch border-b border-[var(--bg3)] bg-[var(--bg0)] sticky top-0 z-10"
          role="tablist"
          aria-label="Card detail tabs"
        >
          {tabs.map((t) => {
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

          <button
            type="button"
            onClick={onToggleRail}
            className="bf-rail-expand"
            aria-label={railExpanded ? 'Collapse rail' : 'Expand rail'}
            aria-pressed={railExpanded}
            title={railExpanded ? 'Collapse rail' : 'Expand rail'}
          >
            <span className="bf-rail-expand-arrow">{railExpanded ? '›‹' : '‹›'}</span>
          </button>
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
