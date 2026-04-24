import type { CSSProperties, ReactNode } from 'react';

/**
 * Shared styling tokens for the card-panel and create-card-panel headers.
 * These used to live duplicated in both files; consolidating here ensures
 * the "display" title style and the dropdown caret icon stay identical
 * across the two panels.
 */

export const headerTitleStyle: CSSProperties = {
  fontFamily: 'var(--font-display)',
  fontWeight: 500,
  fontSize: '23px',
  lineHeight: 1.25,
  letterSpacing: '-0.015em',
  fontVariationSettings: '"opsz" 72',
};

export const HeaderCaret: ReactNode = (
  <svg
    className="w-3 h-3"
    fill="none"
    stroke="currentColor"
    viewBox="0 0 24 24"
    aria-hidden="true"
  >
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
  </svg>
);
