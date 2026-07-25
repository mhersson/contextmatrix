import type { UserSummary } from '../types';

// Code-point-aware first character: string indexing would split surrogate
// pairs (emoji, astral-plane letters) into replacement glyphs.
function firstChar(s: string): string {
  return Array.from(s)[0] ?? '';
}

/**
 * Avatar initials for a roster user. A display name with two or more words
 * yields the first letter of the first and last word; otherwise (empty or
 * single-word display name) the username's first letter.
 */
export function userInitials(displayName: string | undefined, username: string): string {
  const words = (displayName ?? '').trim().split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return (firstChar(words[0]) + firstChar(words[words.length - 1])).toUpperCase();
  }
  return firstChar(username).toUpperCase();
}

/** Human-facing label for a roster user: display name, falling back to username. */
export function userLabel(user: Pick<UserSummary, 'display_name' | 'username'>): string {
  return user.display_name || user.username;
}
