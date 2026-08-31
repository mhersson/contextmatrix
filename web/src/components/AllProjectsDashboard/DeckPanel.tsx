import type { CSSProperties, ReactNode } from 'react';

export type DeckArea = 'projects' | 'agents' | 'models' | 'topcards' | 'activity';

interface DeckPanelProps {
  area: DeckArea;
  /** CSS custom-property reference, e.g. 'var(--blue)'. Drives the left
   *  accent border and the diagonal wash - the board-card status idiom. */
  accent: string;
  title: string;
  meta?: ReactNode;
  children: ReactNode;
}

export function DeckPanel({ area, accent, title, meta, children }: DeckPanelProps) {
  return (
    <section
      className={`apd-panel apd-area-${area}`}
      style={{ '--apd-acc': accent } as CSSProperties}
    >
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">{title}</h2>
        {meta !== undefined && <div className="apd-panel-meta">{meta}</div>}
      </div>
      {children}
    </section>
  );
}
