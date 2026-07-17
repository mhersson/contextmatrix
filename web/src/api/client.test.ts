import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { api, isAPIError, SESSION_EXPIRED_EVENT } from './client';
import type { Card, APIError } from '../types';

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test-project',
  type: 'task',
  state: 'in_progress',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeErrorResponse(code: string, message: string, status: number): Response {
  const error: APIError = { error: message, code };
  return new Response(JSON.stringify(error), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('api.runCard', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(makeResponse(baseCard));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('posts with no body when called without opts', async () => {
    await api.runCard('test-project', 'TEST-001');

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });

  it('posts with no body when opts.interactive is false', async () => {
    await api.runCard('test-project', 'TEST-001', { interactive: false });

    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBeUndefined();
  });

  it('posts {"interactive":true} when opts.interactive is true', async () => {
    await api.runCard('test-project', 'TEST-001', { interactive: true });

    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ interactive: true }));
  });

  it('returns the card from the response', async () => {
    const card = await api.runCard('test-project', 'TEST-001');
    expect(card).toEqual(baseCard);
  });

  it('treats 202 Accepted as success and parses the card', async () => {
    // /run is async; backend returns 202 after queuing the worker trigger.
    fetchSpy.mockResolvedValue(makeResponse(baseCard, 202));
    const card = await api.runCard('test-project', 'TEST-001');
    expect(card).toEqual(baseCard);
  });
});

describe('api.sendCardMessage', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('posts correct JSON and returns parsed response', async () => {
    const mockResponse = { ok: true, message_id: 'msg-123' };
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(makeResponse(mockResponse));

    const result = await api.sendCardMessage('test-project', 'TEST-001', 'Hello agent');

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/projects/test-project/cards/TEST-001/message');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ content: 'Hello agent' }));
    expect(result).toEqual(mockResponse);
  });

  it('surfaces 413 as a typed APIError', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('MESSAGE_TOO_LONG', 'message too long', 413)
    );

    let caught: unknown;
    try {
      await api.sendCardMessage('test-project', 'TEST-001', 'x'.repeat(5000));
    } catch (err) {
      caught = err;
    }

    expect(isAPIError(caught)).toBe(true);
    expect((caught as APIError).code).toBe('MESSAGE_TOO_LONG');
  });
});

describe('api.promoteCardToAutonomous', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('posts to promote endpoint and returns the updated card', async () => {
    const updatedCard = { ...baseCard, autonomous: true };
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(makeResponse(updatedCard));

    const result = await api.promoteCardToAutonomous('test-project', 'TEST-001');

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/projects/test-project/cards/TEST-001/promote');
    expect(init.method).toBe('POST');
    expect(result).toEqual(updatedCard);
  });
});

describe('auth endpoints', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('login POSTs credentials and returns the user', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeResponse({ username: 'alice', display_name: 'Alice', is_admin: true })
    );

    const user = await api.login('alice', 'pw1234567890');

    expect(user.username).toBe('alice');
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/login');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ username: 'alice', password: 'pw1234567890' });
  });

  it('redeemToken POSTs to the token path', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeResponse({ username: 'carol', display_name: '', is_admin: false })
    );

    await api.redeemToken('tok-raw', { password: 'pw1234567890' });

    const [url] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/token/tok-raw');
  });

  it('dispatches session-expired on 401 from a non-auth path', async () => {
    const fired = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, fired);
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('UNAUTHORIZED', 'authentication required', 401)
    );

    await expect(api.getProjects()).rejects.toMatchObject({ code: 'UNAUTHORIZED' });
    expect(fired).toHaveBeenCalledTimes(1);

    window.removeEventListener(SESSION_EXPIRED_EVENT, fired);
  });

  it('does NOT dispatch session-expired for auth-path 401s', async () => {
    const fired = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, fired);

    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('UNAUTHORIZED', 'invalid credentials', 401)
    );
    await expect(api.login('alice', 'wrong')).rejects.toMatchObject({ code: 'UNAUTHORIZED' });

    fetchSpy.mockResolvedValue(makeErrorResponse('UNAUTHORIZED', 'authentication required', 401));
    await expect(api.getAuthSession()).rejects.toMatchObject({ code: 'UNAUTHORIZED' });

    expect(fired).not.toHaveBeenCalled();
    window.removeEventListener(SESSION_EXPIRED_EVENT, fired);
  });
});

describe('admin endpoints', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('adminCreateUser posts to /admin/users and returns the created user plus invite', async () => {
    const mockResponse = {
      user: {
        username: 'alice',
        display_name: 'Alice',
        is_admin: false,
        disabled: false,
        has_password: false,
      },
      invite: {
        token: 'tok-abc123',
        purpose: 'invite',
        expires_at: '2026-07-10T00:00:00Z',
      },
    };
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(makeResponse(mockResponse, 201));

    const result = await api.adminCreateUser({ username: 'alice' });

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/admin/users');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ username: 'alice' });
    expect(result).toEqual(mockResponse);
    expect(result.invite.token).toBe('tok-abc123');
  });

  it('adminPatchUser sends only the fields present in the patch', async () => {
    const updatedUser = {
      username: 'bob',
      display_name: 'Bob',
      is_admin: true,
      disabled: false,
      has_password: true,
    };
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(makeResponse(updatedUser));

    const result = await api.adminPatchUser('bob', { is_admin: true });

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/admin/users/bob');
    expect(init.method).toBe('PATCH');
    // Only the supplied field should be present on the wire - display_name
    // and disabled were omitted by the caller and must not appear as
    // explicit nulls/undefined keys in the JSON body.
    expect(JSON.parse(init.body as string)).toEqual({ is_admin: true });
    expect(result).toEqual(updatedUser);
  });

  it('adminDeleteCredential issues a DELETE request to /admin/credentials/{name}', async () => {
    // A 204 response cannot carry a body per the Fetch spec - construct it
    // directly rather than through makeResponse (which always JSON-encodes).
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));

    const result = await api.adminDeleteCredential('gh-main');

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/admin/credentials/gh-main');
    expect(init.method).toBe('DELETE');
    expect(result).toBeUndefined();
  });
});

