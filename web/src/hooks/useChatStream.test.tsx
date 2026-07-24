import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { ChatMessage } from '../types';
import { useChatStream } from './useChatStream';

const listChatMessagesMock = vi.fn<(id: string, since: number, limit: number) => Promise<{ messages: ChatMessage[] }>>();

vi.mock('../api/client', () => ({
  api: {
    listChatMessages: (...args: Parameters<typeof listChatMessagesMock>) => listChatMessagesMock(...args),
  },
}));

interface MockEventSourceLike {
  onopen?: () => void;
  onmessage?: (e: { data: string }) => void;
  onerror?: () => void;
  close(): void;
  url: string;
}

const instances: MockEventSourceLike[] = [];

class MockES {
  url: string;
  onopen?: () => void;
  onmessage?: (e: { data: string }) => void;
  onerror?: () => void;
  listeners: Record<string, EventListener[]> = {};
  constructor(url: string) {
    this.url = url;
    instances.push(this);
    queueMicrotask(() => this.onopen?.());
  }
  addEventListener(type: string, listener: EventListener) {
    (this.listeners[type] ??= []).push(listener);
  }
  emit(type: string, data: unknown) {
    for (const l of this.listeners[type] ?? []) {
      l({ data: JSON.stringify(data) } as unknown as Event);
    }
  }
  close() {}
}

describe('useChatStream', () => {
  beforeEach(() => {
    (globalThis as unknown as { EventSource: typeof MockES }).EventSource = MockES;
    instances.length = 0;
    listChatMessagesMock.mockReset();
    listChatMessagesMock.mockResolvedValue({ messages: [] });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('connects and reports connected=true', async () => {
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));
    expect(instances).toHaveLength(1);
    expect(instances[0].url).toContain('/api/chats/S1/stream');
    expect(instances[0].url).toContain('since_seq=0');
  });

  it('appends incoming messages to logs', async () => {
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));
    act(() => {
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 1, role: 'user', content: 'hi' }) });
    });
    await waitFor(() => expect(result.current.logs).toHaveLength(1));
    expect(result.current.logs[0].content).toBe('hi');
    expect(result.current.logs[0].type).toBe('user');
    expect(result.current.logs[0].seq).toBe(1);
  });

  it('maps assistant_text role to text type', async () => {
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));
    act(() => {
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 2, role: 'assistant_text', content: 'hello' }) });
    });
    await waitFor(() => expect(result.current.logs).toHaveLength(1));
    expect(result.current.logs[0].type).toBe('text');
  });

  it('ignores malformed payloads', async () => {
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));
    act(() => {
      instances[0].onmessage?.({ data: 'not-json' });
    });
    expect(result.current.logs).toHaveLength(0);
  });

  it('bootstraps persisted transcript before subscribing SSE', async () => {
    listChatMessagesMock.mockResolvedValue({
      messages: [
        { id: 1, session_id: 'S1', seq: 1, role: 'user', content: 'past1', created_at: '2026-05-14T00:00:00Z' },
        { id: 2, session_id: 'S1', seq: 2, role: 'assistant_text', content: 'past2', created_at: '2026-05-14T00:00:01Z' },
      ],
    });

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.logs).toHaveLength(2));

    expect(listChatMessagesMock).toHaveBeenCalledWith('S1', 0, 1000);
    expect(result.current.logs[0].content).toBe('past1');
    expect(result.current.logs[1].content).toBe('past2');

    // SSE subscribes with since_seq=2 so it only delivers strictly newer events.
    await waitFor(() => expect(instances).toHaveLength(1));
    expect(instances[0].url).toContain('since_seq=2');
  });

  it('dedups SSE events whose seq is <= last bootstrap seq', async () => {
    listChatMessagesMock.mockResolvedValue({
      messages: [
        { id: 1, session_id: 'S1', seq: 1, role: 'user', content: 'past1', created_at: '2026-05-14T00:00:00Z' },
        { id: 2, session_id: 'S1', seq: 2, role: 'assistant_text', content: 'past2', created_at: '2026-05-14T00:00:01Z' },
      ],
    });

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));

    act(() => {
      // SSE replays seq=2 (already in bootstrap) and delivers a fresh seq=3.
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 2, role: 'assistant_text', content: 'past2-dup' }) });
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 3, role: 'assistant_text', content: 'new3' }) });
    });

    await waitFor(() => expect(result.current.logs).toHaveLength(3));
    expect(result.current.logs.map((l) => l.content)).toEqual(['past1', 'past2', 'new3']);
  });

  it('continues with SSE only when bootstrap fetch fails', async () => {
    listChatMessagesMock.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));

    act(() => {
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 1, role: 'user', content: 'hi' }) });
    });
    await waitFor(() => expect(result.current.logs).toHaveLength(1));
  });

  it('parses assistant_working fields from session_updated events', async () => {
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));

    act(() => {
      (instances[0] as unknown as MockES).emit('session_updated', {
        assistant_working: true,
        assistant_working_since: '2026-07-24T10:00:00Z',
      });
    });
    await waitFor(() => expect(result.current.sessionUpdate?.assistant_working).toBe(true));
    expect(result.current.sessionUpdate?.assistant_working_since).toBe('2026-07-24T10:00:00Z');

    act(() => {
      (instances[0] as unknown as MockES).emit('session_updated', { assistant_working: false });
    });
    await waitFor(() => expect(result.current.sessionUpdate?.assistant_working).toBe(false));
  });
});
