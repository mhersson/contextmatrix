import { isAPIError } from '../api/client';

/**
 * Best-effort human-readable message from a caught API error; the fallback
 * covers network-shaped failures with no structured body. Single source for
 * every admin surface that flattens errors into banner text.
 */
export function errorMessage(err: unknown, fallback: string): string {
  return isAPIError(err) ? err.error : fallback;
}
