import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { api, isAPIError } from './client';
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
    // /run is async; backend returns 202 after queuing the runner trigger.
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

  it('surfaces 409 as a typed APIError', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('NOT_RUNNING', 'session not running', 409)
    );

    let caught: unknown;
    try {
      await api.sendCardMessage('test-project', 'TEST-001', 'hello');
    } catch (err) {
      caught = err;
    }

    expect(isAPIError(caught)).toBe(true);
    expect((caught as APIError).code).toBe('NOT_RUNNING');
  });

  it('surfaces 503 as a typed APIError', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('RUNNER_DISABLED', 'runner disabled', 503)
    );

    let caught: unknown;
    try {
      await api.sendCardMessage('test-project', 'TEST-001', 'hello');
    } catch (err) {
      caught = err;
    }

    expect(isAPIError(caught)).toBe(true);
    expect((caught as APIError).code).toBe('RUNNER_DISABLED');
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

  it('surfaces 409 as a typed APIError', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('RUNNER_NOT_RUNNING', 'card is not currently running', 409)
    );

    let caught: unknown;
    try {
      await api.promoteCardToAutonomous('test-project', 'TEST-001');
    } catch (err) {
      caught = err;
    }

    expect(isAPIError(caught)).toBe(true);
    expect((caught as APIError).code).toBe('RUNNER_NOT_RUNNING');
  });

  it('surfaces 503 as a typed APIError', async () => {
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      makeErrorResponse('RUNNER_DISABLED', 'runner disabled', 503)
    );

    let caught: unknown;
    try {
      await api.promoteCardToAutonomous('test-project', 'TEST-001');
    } catch (err) {
      caught = err;
    }

    expect(isAPIError(caught)).toBe(true);
    expect((caught as APIError).code).toBe('RUNNER_DISABLED');
  });
});
