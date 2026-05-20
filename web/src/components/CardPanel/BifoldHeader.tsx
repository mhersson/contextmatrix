import type { ReactNode } from 'react';

export interface BifoldHeaderProps {
  title: ReactNode;
  chips?: ReactNode;
  actions?: ReactNode;
}

/**
 * Shared header chrome for card panels (CardPanelHeader + CreateCardPanel).
 * Provides the canonical bifold layout: a left column with chips + title, and
 * a right cluster of action buttons.
 *
 * Slots:
 *   - title   — the title element (input or heading, filled by the caller)
 *   - chips   — optional row rendered above the title (state/type/priority chips, close button, etc.)
 *   - actions — optional cluster rendered at the right of the header
 */
export function BifoldHeader({ title, chips, actions }: BifoldHeaderProps) {
  return (
    <div className="flex flex-wrap items-start gap-x-4 gap-y-3 px-5 py-4 border-b border-[var(--bg3)]">
      {/* Title column — flex: 1 1 340px + min-width: 0 so it shrinks before wrapping the cluster. */}
      <div className="flex-1 min-w-0 flex flex-col gap-2" style={{ flexBasis: '340px' }}>
        {chips}
        {title}
      </div>

      {/* Action cluster — wraps to the next line (still right-aligned via ml-auto) when crowded. */}
      {actions && (
        <div className="flex items-center gap-2 ml-auto shrink-0 flex-wrap justify-end">
          {actions}
        </div>
      )}
    </div>
  );
}
