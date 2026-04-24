import type { CSSProperties } from 'react';

/**
 * Shared palette tokens + chip helpers for card type / priority / state
 * chips. These values are the single source of truth — every component that
 * renders a chip (board CardItem, card-panel header, create-card panel)
 * imports from here. Never duplicate these maps locally; a prior drift
 * between two copies had subtask colors diverging between the board and
 * the panel, which is exactly the class of bug we're avoiding.
 *
 * Values are CSS custom-property references (e.g. `var(--aqua)`) so every
 * palette (Everforest / Radix / Catppuccin) gets the right color without
 * the helper knowing about the palette system.
 */

export const typeColors: Record<string, string> = {
  task: 'var(--blue)',
  bug: 'var(--red)',
  feature: 'var(--green)',
  subtask: 'var(--aqua)',
};

export const priorityColors: Record<string, string> = {
  critical: 'var(--red)',
  high: 'var(--orange)',
  medium: 'var(--yellow)',
  low: 'var(--grey1)',
};

export const stateColors: Record<string, string> = {
  todo: 'var(--grey2)',
  in_progress: 'var(--yellow)',
  hitl: 'var(--aqua)',
  review: 'var(--blue)',
  done: 'var(--green)',
  blocked: 'var(--orange)',
  stalled: 'var(--red)',
  not_planned: 'var(--grey1)',
};

/**
 * Tints a chip with a translucent background derived from its accent color.
 * Intentionally does NOT accept user input — every caller passes a constant
 * from one of the maps above, or `var(--grey1)` as a fallback. Don't route
 * untrusted values through this helper; it interpolates directly into a
 * `color-mix(...)` CSS expression.
 */
export function chipTint(color: string): CSSProperties {
  return {
    backgroundColor: `color-mix(in srgb, ${color} 22%, transparent)`,
    color,
  };
}

/**
 * Strips the project prefix from a card ID for compact display in tight
 * contexts (collapsed card headers, parent badges). Always returns a
 * non-empty string; falls back to the full ID when no `-` is present.
 */
export function shortCardId(id: string): string {
  const dash = id.lastIndexOf('-');
  return dash >= 0 ? id.slice(dash + 1) : id;
}
