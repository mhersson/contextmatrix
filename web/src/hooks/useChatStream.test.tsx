import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { ChatMessage } from '../types';
import { useChatStream } from './useChatStream';

const listChatMessagesMock = vi.fn<(id: string, since: number, limit: number) => Promise<{ messages: ChatMessage[] }>>();
const listChatMessagesTailMock = vi.fn<(id: string, limit: number) => Promise<{ messages: ChatMessage[] }>>();
const listChatMessagesBeforeMock = vi.fn<(id: string, beforeSeq: number, limit: number) => Promise<{ messages: ChatMessage[] }>>();

vi.mock('../api/client', () => ({
  api: {
    listChatMessages: (...args: Parameters<typeof listChatMessagesMock>) => listChatMessagesMock(...args),
    listChatMessagesTail: (...args: Parameters<typeof listChatMessagesTailMock>) => listChatMessagesTailMock(...args),
    listChatMessagesBefore: (...args: Parameters<typeof listChatMessagesBeforeMock>) => listChatMessagesBeforeMock(...args),
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

function makeMessage(seq: number, role = 'user', content = `m${seq}`): ChatMessage {
  return {
    id: seq,
    session_id: 'S1',
    seq,
    role,
    content,
    created_at: `2026-05-14T00:00:${String(seq % 60).padStart(2, '0')}Z`,
  } as ChatMessage;
}

describe('useChatStream', () => {
  beforeEach(() => {
    (globalThis as unknown as { EventSource: typeof MockES }).EventSource = MockES;
    instances.length = 0;
    listChatMessagesMock.mockReset();
    listChatMessagesMock.mockResolvedValue({ messages: [] });
    listChatMessagesTailMock.mockReset();
    listChatMessagesTailMock.mockResolvedValue({ messages: [] });
    listChatMessagesBeforeMock.mockReset();
    listChatMessagesBeforeMock.mockResolvedValue({ messages: [] });
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

  it('bootstraps the NEWEST page before subscribing SSE and reports hasMore', async () => {
    // A long session: the newest page starts at seq 21, so history remains.
    listChatMessagesTailMock.mockResolvedValue({
      messages: Array.from({ length: 10 }, (_, i) => makeMessage(21 + i)),
    });

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.logs).toHaveLength(10));

    expect(listChatMessagesTailMock).toHaveBeenCalledWith('S1', 200);
    expect(result.current.logs[0].seq).toBe(21);
    expect(result.current.logs[9].seq).toBe(30);
    expect(result.current.hasMore).toBe(true);

    // SSE subscribes with since_seq=30 so it only delivers strictly newer events.
    await waitFor(() => expect(instances).toHaveLength(1));
    expect(instances[0].url).toContain('since_seq=30');
  });

  it('reports hasMore=false when the bootstrap page starts at seq 1', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: [makeMessage(1), makeMessage(2)],
    });

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.logs).toHaveLength(2));

    expect(result.current.hasMore).toBe(false);
  });

  it('dedups SSE events whose seq is <= last bootstrap seq', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: [makeMessage(1, 'user', 'past1'), makeMessage(2, 'assistant_text', 'past2')],
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
    listChatMessagesTailMock.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.connected).toBe(true));

    act(() => {
      instances[0].onmessage?.({ data: JSON.stringify({ seq: 1, role: 'user', content: 'hi' }) });
    });
    await waitFor(() => expect(result.current.logs).toHaveLength(1));
    expect(result.current.hasMore).toBe(false);
  });

  it('loadOlder pages backward, prepends in order, and exhausts at seq 1', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: Array.from({ length: 10 }, (_, i) => makeMessage(21 + i)),
    });
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.hasMore).toBe(true));

    listChatMessagesBeforeMock.mockResolvedValueOnce({
      messages: Array.from({ length: 10 }, (_, i) => makeMessage(11 + i)),
    });
    await act(async () => {
      await result.current.loadOlder();
    });

    expect(listChatMessagesBeforeMock).toHaveBeenCalledWith('S1', 21, 200);
    expect(result.current.logs.map((l) => l.seq)).toEqual(
      Array.from({ length: 20 }, (_, i) => 11 + i),
    );
    expect(result.current.hasMore).toBe(true);

    listChatMessagesBeforeMock.mockResolvedValueOnce({
      messages: Array.from({ length: 10 }, (_, i) => makeMessage(1 + i)),
    });
    await act(async () => {
      await result.current.loadOlder();
    });

    expect(listChatMessagesBeforeMock).toHaveBeenLastCalledWith('S1', 11, 200);
    expect(result.current.logs).toHaveLength(30);
    expect(result.current.logs[0].seq).toBe(1);
    expect(result.current.hasMore).toBe(false);

    // Exhausted: further calls never hit the API.
    const calls = listChatMessagesBeforeMock.mock.calls.length;
    await act(async () => {
      await result.current.loadOlder();
    });
    expect(listChatMessagesBeforeMock.mock.calls.length).toBe(calls);
  });

  it('serializes concurrent loadOlder calls to one in-flight fetch', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: [makeMessage(21), makeMessage(22)],
    });
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.hasMore).toBe(true));

    let resolveFetch: (v: { messages: ChatMessage[] }) => void = () => {};
    listChatMessagesBeforeMock.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveFetch = resolve;
      }),
    );

    let first: Promise<void> = Promise.resolve();
    let second: Promise<void> = Promise.resolve();
    act(() => {
      first = result.current.loadOlder();
      second = result.current.loadOlder();
    });
    await waitFor(() => expect(result.current.loadingOlder).toBe(true));

    await act(async () => {
      resolveFetch({ messages: [makeMessage(20)] });
      await Promise.all([first, second]);
    });

    expect(listChatMessagesBeforeMock).toHaveBeenCalledTimes(1);
  });

  it('preserves divider and rehydration fields across a history page boundary', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: [makeMessage(10, 'assistant_text', 'after-clear')],
    });
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.logs).toHaveLength(1));

    listChatMessagesBeforeMock.mockResolvedValueOnce({
      messages: [
        { ...makeMessage(8, 'user', 'old'), rehydration_phase: true },
        { ...makeMessage(9, 'system', 'Context cleared'), kind: 'divider' },
      ] as ChatMessage[],
    });
    await act(async () => {
      await result.current.loadOlder();
    });

    expect(result.current.logs.map((l) => l.seq)).toEqual([8, 9, 10]);
    expect(result.current.logs[0].rehydration_phase).toBe(true);
    expect(result.current.logs[1].kind).toBe('divider');
  });

  it('maps tool_result and tool_result_summary roles to the tool_result type', async () => {
    listChatMessagesTailMock.mockResolvedValue({
      messages: [
        makeMessage(1, 'tool_result', 'raw output'),
        makeMessage(2, 'tool_result_summary', 'summarized output'),
      ],
    });
    const { result } = renderHook(() => useChatStream('S1'));
    await waitFor(() => expect(result.current.logs).toHaveLength(2));

    expect(result.current.logs.every((l) => l.type === 'tool_result')).toBe(true);
  });

  it('coalesces a synchronous SSE burst into at most two commits', async () => {
    let renders = 0;
    const { result } = renderHook(() => {
      renders++;
      return useChatStream('S1');
    });
    await waitFor(() => expect(result.current.connected).toBe(true));
    const rendersBefore = renders;

    act(() => {
      for (let seq = 1; seq <= 5; seq++) {
        instances[0].onmessage?.({ data: JSON.stringify({ seq, role: 'user', content: `m${seq}` }) });
      }
    });

    await waitFor(() => expect(result.current.logs).toHaveLength(5));
    // The ring buffer's 50 ms coalescing window publishes the burst as one
    // notification (a second commit may come from unrelated state).
    expect(renders - rendersBefore).toBeLessThanOrEqual(2);
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
