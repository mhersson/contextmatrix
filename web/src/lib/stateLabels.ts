/**
 * UI-only display labels for card states. The state values on disk and on
 * the wire stay unchanged ("todo", "in_progress", etc.); this map only
 * affects rendering.
 *
 * Don't reach for this directly when you need transition logic - the state
 * machine still keys on the raw values. This is presentation only.
 */
const STATE_DISPLAY_LABEL: Record<string, string> = {
  todo: 'Backlog',
  in_progress: 'In Progress',
  review: 'In Review',
  done: 'Shipped',
  stalled: 'Stalled',
  not_planned: 'Not Planned',
};

/**
 * Resolves a raw state value to its UI label, falling back to a
 * title-cased word-split for project-defined custom states.
 */
export function displayState(state: string): string {
  const known = STATE_DISPLAY_LABEL[state];
  if (known) return known;
  return state
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}
