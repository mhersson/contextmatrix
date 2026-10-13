import { describe, it, expect, vi } from 'vitest';
import { errorWithDetails } from './errors';

vi.mock('../api/client', () => ({
  isAPIError: (err: unknown) => err != null && typeof err === 'object' && 'error' in err,
}));

describe('errorWithDetails', () => {
  it.each([
    ['bad request with details shows only the details', { error: 'invalid import request', code: 'BAD_REQUEST', details: 'at most 200 issues per import' }, 'at most 200 issues per import'],
    ['other error with details joins both', { error: 'Jira refused the request', code: 'JIRA_REQUEST_REJECTED', details: 'Field x is invalid' }, 'Jira refused the request: Field x is invalid'],
    ['error without details shows the error', { error: 'imports are paused', code: 'X' }, 'imports are paused'],
    ['bad request without details shows the error', { error: 'invalid import request', code: 'BAD_REQUEST' }, 'invalid import request'],
    ['non-API error falls back', new TypeError('Failed to fetch'), 'fallback'],
  ])('%s', (_name, err, want) => {
    expect(errorWithDetails(err, 'fallback')).toBe(want);
  });
});
