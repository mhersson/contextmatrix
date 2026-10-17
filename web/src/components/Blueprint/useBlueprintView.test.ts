import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { useChatStream } from '../../hooks/useChatStream';
import { refreshArtifact } from './artifactCache';
import { useBlueprintView } from './useBlueprintView';
import { viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import type { BlueprintView } from '../../types';

vi.mock('../../api/client', () => ({
  api: { getBlueprintView: vi.fn(), listChatMessagesTail: vi.fn(), listChatMessagesBefore: vi.fn() },
  isAPIError: (e: unknown) => typeof e === 'object' && e !== null && typeof (e as { error?: unknown }).error === 'string',
}));
vi.mock('./artifactCache', () => ({ refreshArtifact: vi.fn() }));

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }
  addEventListener() {}
  close() {
    this.closed = true;
  }
}

const getView = vi.mocked(api.getBlueprintView);
const stream = () => MockEventSource.instances[MockEventSource.instances.length - 1];
// CM's wire shape: an unnamed chat stream event of kind "blueprint" whose content is the frame.
const send = (frame: unknown) =>
  act(() =>
    stream().onmessage?.({
      data: JSON.stringify({ seq: 0, role: '', content: typeof frame === 'string' ? frame : JSON.stringify(frame), kind: 'blueprint' }),
    }),
  );
const withSeq = (seq: number, spent = 0.16): BlueprintView => {
  const view = viewAtDesignGate();
  return { ...view, seq, cost: { ...view.cost, spent_usd: spent } };
};
const draft = (offset: number, text: string) => ({ stage: 'solution_design', version: 3, offset, text });
const open = () => act(() => stream().onopen?.());
const reconnect = () => {
  vi.useFakeTimers();
  act(() => {
    stream().onerror?.();
    vi.advanceTimersByTime(1000);
  });
  vi.useRealTimers();
};
// A transcript row on the same stream; marker and prompt rows are never gated.
const row = (seq: number, kind?: string) =>
  act(() => stream().onmessage?.({ data: JSON.stringify({ seq, role: kind === 'marker' ? 'system' : 'assistant_text', content: '{}', kind }) }));

// ChatThread's wiring: the chat stream it already holds feeds the blueprint view.
function usePane(chatId: string) {
  const chat = useChatStream(chatId);
  return { chat, blueprint: useBlueprintView(chatId, chat.subscribeBlueprint, chat.connection, chat.logs) };
}

async function renderPane() {
  const hook = renderHook(({ id }) => usePane(id), { initialProps: { id: 'S1' } });
  await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
  return hook;
}

describe('useBlueprintView', () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    vi.stubGlobal('EventSource', MockEventSource);
    vi.mocked(api.listChatMessagesTail).mockResolvedValue({ messages: [] });
    getView.mockReset();
    vi.mocked(refreshArtifact).mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('loads the view and applies snapshots from the chat stream', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    send({ type: 'snapshot', view: withSeq(43, 0.2) });
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 43, cost: { spent_usd: 0.2 } } });
    expect(MockEventSource.instances.map((es) => es.url)).toEqual(['/api/chats/S1/stream?since_seq=0']);
  });

  it('ignores a GET answer older than the snapshot it holds', async () => {
    let answer: (v: BlueprintView) => void = () => {};
    getView.mockReturnValue(new Promise((resolve) => { answer = resolve; }));
    const { result } = await renderPane();
    send({ type: 'snapshot', view: withSeq(50, 0.5) });
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 50 } });
    await act(async () => answer(withSeq(42)));
    expect(result.current.blueprint.state).toMatchObject({ view: { seq: 50 } });
  });

  it('treats missing arrays as empty in a fetched view and in a snapshot', async () => {
    getView.mockResolvedValue({ ...withSeq(1), pipeline: null, artifacts: null } as unknown as BlueprintView);
    const { result } = await renderPane();
    await waitFor(() =>
      expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { pipeline: [], artifacts: [] } }),
    );
    const view = withSeq(2);
    const artifacts = view.artifacts.map((a) => (a.review ? { ...a, review: { ...a.review, findings: null } } : a));
    send({ type: 'snapshot', view: { ...view, pipeline: null, artifacts } });
    expect(result.current.blueprint.state).toMatchObject({
      view: { seq: 2, pipeline: [], artifacts: expect.arrayContaining([expect.objectContaining({ review: expect.objectContaining({ findings: [] }) })]) },
    });
  });

  it('appends to an emoji draft by its code-point length and drops a gap', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    send({ type: 'snapshot', view: withSeq(43), draft: draft(0, 'A \u{1F44D}') });
    send({ type: 'draft', draft: draft(3, ' b') });
    send({ type: 'draft', draft: draft(99, ' lost') });
    expect(result.current.blueprint.state).toMatchObject({ draft: { text: 'A \u{1F44D} b', length: 5 } });
    send({ type: 'draft', draft: draft(0, 'Again') });
    expect(result.current.blueprint.state).toMatchObject({ draft: { text: 'Again', length: 5 } });
  });

  it('drops the held draft on a snapshot without one', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    send({ type: 'snapshot', view: withSeq(43), draft: draft(0, 'Writing') });
    expect(result.current.blueprint.state).toMatchObject({ draft: { text: 'Writing' } });
    send({ type: 'snapshot', view: withSeq(44) });
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 44 }, draft: null });
  });

  it('refetches a stored artifact', async () => {
    getView.mockResolvedValue(withSeq(42));
    await renderPane();
    send({ type: 'artifact_stored', stage: 'emission', version: 1 });
    expect(refreshArtifact).toHaveBeenCalledWith('S1', 'emission', 1);
  });

  // A slow reader can lose a snapshot, and a reconnect may bring no blueprint frame at all.
  it('refetches the view and a cached board when the stream reopens', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    open();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    expect(getView).toHaveBeenCalledTimes(1);
    expect(refreshArtifact).not.toHaveBeenCalled();

    getView.mockResolvedValue(withSeq(45, 0.3));
    reconnect();
    open();
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 42 } });
    await waitFor(() => expect(result.current.blueprint.state).toMatchObject({ view: { seq: 45 } }));
    expect(getView).toHaveBeenCalledTimes(2);
    expect(refreshArtifact).toHaveBeenCalledTimes(1);
    expect(refreshArtifact).toHaveBeenCalledWith('S1', 'emission', 1);
  });

  it.each(['marker', 'prompt'])('refetches the view when a %s row arrives', async (kind) => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    row(5, kind);
    await waitFor(() => expect(getView).toHaveBeenCalledTimes(2));
  });

  it('does not refetch the view for an ordinary row', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    row(5);
    await waitFor(() => expect(result.current.chat.logs).toHaveLength(1));
    expect(getView).toHaveBeenCalledTimes(1);
  });

  it('ignores a refetched view older than the snapshot it holds', async () => {
    getView.mockResolvedValueOnce(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    send({ type: 'snapshot', view: withSeq(50) });
    let answer: (v: BlueprintView) => void = () => {};
    getView.mockReturnValueOnce(new Promise((resolve) => { answer = resolve; }));
    row(5, 'marker');
    await waitFor(() => expect(getView).toHaveBeenCalledTimes(2));
    await act(async () => answer(withSeq(44)));
    expect(result.current.blueprint.state).toMatchObject({ view: { seq: 50 } });
  });

  it('drops a frame that does not parse', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
    send('{"type":"snapshot","view":');
    send('null');
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 42 } });
    send({ type: 'snapshot', view: withSeq(43) });
    expect(result.current.blueprint.state).toMatchObject({ view: { seq: 43 } });
  });

  it('takes the snapshot that leads a reconnected stream', async () => {
    getView.mockResolvedValue(withSeq(42));
    const { result } = await renderPane();
    send({ type: 'snapshot', view: withSeq(43), draft: draft(0, 'Before the drop') });
    vi.useFakeTimers();
    act(() => {
      stream().onerror?.();
      vi.advanceTimersByTime(1000);
    });
    vi.useRealTimers();
    expect(MockEventSource.instances).toHaveLength(2);
    send({ type: 'snapshot', view: withSeq(47), draft: draft(0, 'Before the drop and after') });
    send({ type: 'draft', draft: draft(25, '!') });
    expect(result.current.blueprint.state).toMatchObject({ view: { seq: 47 }, draft: { text: 'Before the drop and after!' } });
  });

  it.each(['BLUEPRINT_NOT_FOUND', 'CHAT_NOT_FOUND', 'CHAT_KIND_UNAVAILABLE'])('renders as today on %s', async (code) => {
    getView.mockRejectedValue({ error: 'no', code });
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('absent'));
  });

  it.each([
    ['a 502', { error: 'down', code: 'BACKEND_UNAVAILABLE' }],
    ['a 404', { error: 'no', code: 'BLUEPRINT_NOT_FOUND' }],
  ])('keeps an absent pane plain when a refetch gets %s', async (_name, failure) => {
    getView.mockRejectedValueOnce({ error: 'no', code: 'BLUEPRINT_NOT_FOUND' });
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('absent'));
    getView.mockRejectedValueOnce(failure);
    row(5, 'marker');
    await waitFor(() => expect(getView).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(result.current.blueprint.state.status).toBe('absent');
  });

  it('shows an unreachable architect when the first GET gets a 502', async () => {
    getView.mockRejectedValueOnce({ error: 'down', code: 'BACKEND_UNAVAILABLE' });
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('unreachable'));
  });

  it('treats an answer without a body like an unreachable architect', async () => {
    getView.mockResolvedValue(undefined as unknown as BlueprintView);
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('unreachable'));
  });

  it('offers a retry when the architect is unreachable', async () => {
    getView.mockRejectedValueOnce({ error: 'down', code: 'BACKEND_UNAVAILABLE' }).mockResolvedValueOnce(withSeq(42));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('unreachable'));
    act(() => result.current.blueprint.retry());
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('ready'));
  });

  // The proxy ends a read that stalls mid-body as a truncated 200; its body does not parse.
  it('treats a truncated answer like an unreachable architect', async () => {
    getView.mockRejectedValue(new SyntaxError('Unexpected end of JSON input'));
    const { result } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('unreachable'));
  });

  it('lets a snapshot outrank a failed fetch either way round', async () => {
    let fail: (e: unknown) => void = () => {};
    getView.mockReturnValueOnce(new Promise((_, reject) => { fail = reject; }));
    const { result } = await renderPane();
    send({ type: 'snapshot', view: withSeq(43) });
    await act(async () => fail({ error: 'down', code: 'BACKEND_UNAVAILABLE' }));
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 43 } });

    getView.mockRejectedValueOnce({ error: 'down', code: 'BACKEND_UNAVAILABLE' });
    act(() => result.current.blueprint.retry());
    await waitFor(() => expect(result.current.blueprint.state.status).toBe('unreachable'));
    send({ type: 'snapshot', view: withSeq(44) });
    expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { seq: 44 } });
  });

  it('starts over for another chat', async () => {
    getView.mockImplementation((id) => Promise.resolve({ ...withSeq(42), id }));
    const { result, rerender } = await renderPane();
    await waitFor(() => expect(result.current.blueprint.state).toMatchObject({ view: { id: 'S1' } }));
    rerender({ id: 'S2' });
    expect(result.current.blueprint.state.status).toBe('loading');
    await waitFor(() => expect(result.current.blueprint.state).toMatchObject({ status: 'ready', view: { id: 'S2' } }));
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(2));
    expect(MockEventSource.instances[0].closed).toBe(true);
    send({ type: 'snapshot', view: { ...withSeq(50), id: 'S2' } });
    expect(result.current.blueprint.state).toMatchObject({ view: { id: 'S2', seq: 50 } });
  });
});
