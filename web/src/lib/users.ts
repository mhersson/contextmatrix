import type { UserSummary } from '../types';

/**
 * Avatar initials for a roster user. A display name with two or more words
 * yields the first letter of the first and last word; otherwise (empty or
 * single-word display name) the username's first letter.
 */
export function userInitials(displayName: string | undefined, username: string): string {
  const words = (displayName ?? '').trim().split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return (words[0][0] + words[words.length - 1][0]).toUpperCase();
  }
  return username.charAt(0).toUpperCase();
}

/** Human-facing label for a roster user: display name, falling back to username. */
export function userLabel(user: Pick<UserSummary, 'display_name' | 'username'>): string {
  return user.display_name || user.username;
}
