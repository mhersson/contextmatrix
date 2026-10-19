import type { ReactNode } from 'react';

/** The technical text behind a plain one, folded for a technical helper; draws nothing for a blank detail. */
export function TechnicalDetail({ detail, render }: { detail?: string; render?: (detail: string) => ReactNode }) {
  if (!detail || detail.trim() === '') return null;
  return (
    <details className="tech-detail">
      <summary>Technical detail</summary>
      <div>{render ? render(detail) : detail}</div>
    </details>
  );
}
