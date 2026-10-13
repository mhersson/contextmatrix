import { isAPIError } from '../api/client';

/**
 * Best-effort human-readable message from a caught API error; the fallback
 * covers network-shaped failures with no structured body. Single source for
 * every admin surface that flattens errors into banner text.
 */
export function errorMessage(err: unknown, fallback: string): string {
  return isAPIError(err) ? err.error : fallback;
}

/**
 * Like errorMessage, but keeps the server's `details`: a BAD_REQUEST's details
 * are the actionable part, any other error's details follow its message.
 */
export function errorWithDetails(err: unknown, fallback: string): string {
  if (!isAPIError(err)) return fallback;
  if (!err.details) return err.error;
  return err.code === 'BAD_REQUEST' ? err.details : `${err.error}: ${err.details}`;
}
