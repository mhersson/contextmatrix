import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { resetMockupStore, useMockupSettings } from '../../components/Mockup/mockupStore';
import { viewAtDesignGate, viewAtMockup } from '../../test-utils/blueprintFixtures';
import type { ChatSession } from '../../types';
import { rawRound, roundEntry } from '../../test-utils/roundFixtures';
import { useChatLiveData } from '../../hooks/useChatLiveData';
import { ChatThread } from './ChatThread';
import { MemoryRouter } from 'react-router';
import { HandoffContext } from '../../components/ChatPanel/handoffContext';

vi.mock('../../api/client', () => ({
  api: {
    getChat: vi.fn(),
    sendChatMessage: vi.fn(),
    listChatMessagesTail: vi.fn(),
    listChatMessagesBefore: vi.fn(),
    getBlueprintView: vi.fn(),
    getBlueprintArtifact: vi.fn(),
  },
  isAPIError: vi.fn(() => false),
}));

// Chat entries lazy-load the markdown chunk; loading it up front keeps the
// import from finishing after this file's environment is torn down.
beforeAll(() => import('../../components/Markdown/SafeMarkdownPreview'));

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }
  addEventListener() {}
  close() {}
}

const session: ChatSession = {
  id: 'S1',
  title: 'Blueprint',
  kind: 'architect',
  status: 'active',
  created_at: '2026-10-17T10:00:00Z',
  last_active: '2026-10-17T10:00:00Z',
  created_by: 'human:web',
};

const emit = (event: object) => act(() => MockEventSource.instances[0].onmessage?.({ data: JSON.stringify(event) }));

describe('ChatThread on an architect session', () => {
  beforeEach(() => {
    resetMockupStore();
    MockEventSource.instances = [];
    vi.stubGlobal('EventSource', MockEventSource);
    vi.mocked(api.getChat).mockResolvedValue(session);
    vi.mocked(api.listChatMessagesTail).mockResolvedValue({ messages: [] });
    // The view fetch never answers, so only the stream can draw the pane.
    vi.mocked(api.getBlueprintView).mockClear().mockReturnValue(new Promise(() => {}));
  });
  afterEach(() => vi.unstubAllGlobals());

  it('draws the pane from blueprint frames on the chat stream it already holds', async () => {
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(api.getBlueprintView).toHaveBeenCalledWith('S1'));
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));

    const snapshot = { type: 'snapshot', view: { ...viewAtDesignGate(), cost: { ...viewAtDesignGate().cost, spent_usd: 0.4 } } };
    emit({ seq: 0, role: '', content: JSON.stringify(snapshot), kind: 'blueprint' });
    expect(await screen.findByRole('list', { name: 'Blueprint stages' })).toBeInTheDocument();
    expect(screen.getByText('$0.40 of $2.00')).toBeInTheDocument();

    emit({ seq: 1, role: 'assistant_text', content: 'Design rev 2 is ready for you.' });
    expect(await screen.findByText('Design rev 2 is ready for you.')).toBeInTheDocument();
    expect(screen.queryByText(/"type":"snapshot"/)).not.toBeInTheDocument();
    expect(MockEventSource.instances.map((es) => es.url)).toEqual(['/api/chats/S1/stream?since_seq=0']);
  });

  it("never publishes CM's cost estimate for the pane header", async () => {
    vi.mocked(api.getChat).mockResolvedValue({ ...session, model: 'm-1', estimated_cost_usd: 0.5 });
    const { result } = renderHook(() => useChatLiveData('S1'));
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(result.current?.model).toBe('m-1'));
    expect(result.current?.estimatedCostUsd).toBeUndefined();
  });

  it('refetches the view when its chat stream reopens', async () => {
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    act(() => MockEventSource.instances[0].onopen?.());
    expect(api.getBlueprintView).toHaveBeenCalledTimes(1);

    vi.useFakeTimers();
    act(() => {
      MockEventSource.instances[0].onerror?.();
      vi.advanceTimersByTime(1000);
    });
    vi.useRealTimers();
    act(() => MockEventSource.instances[1].onopen?.());
    await waitFor(() => expect(api.getBlueprintView).toHaveBeenCalledTimes(2));
  });

  it('sends a question round as a structured reply', async () => {
    vi.mocked(api.sendChatMessage).mockResolvedValue({ ok: true, message_id: 'm1' });
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    act(() => MockEventSource.instances[0].onopen?.());
    const frame = roundEntry(1, 'round:1', rawRound({ answers: [{ number: 1, option: 1 }, { number: 2, option: 1 }] }));
    emit({ seq: 1, role: 'assistant_text', content: frame.content, kind: 'prompt' });
    fireEvent.click(await screen.findByRole('tab', { name: /Postgres/ }));
    fireEvent.click(screen.getByRole('button', { name: /CloudNativePG/ }));
    fireEvent.click(screen.getByRole('tab', { name: /Submit/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Submit all 3' }));
    await waitFor(() =>
      expect(api.sendChatMessage).toHaveBeenCalledWith('S1', expect.any(String), {
        round: {
          prompt_id: 'round:1',
          answers: [{ number: 1, option: 1 }, { number: 2, option: 1 }, { number: 3, option: 1 }],
          decisions: [],
          final: true,
        },
      }),
    );
  });

  it('sends the round chat as a message about the waiting round', async () => {
    vi.mocked(api.sendChatMessage).mockClear().mockResolvedValue({ ok: true, message_id: 'm1' });
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    act(() => MockEventSource.instances[0].onopen?.());
    const frame = roundEntry(1, 'round:2', rawRound());
    emit({ seq: 1, role: 'assistant_text', content: frame.content, kind: 'prompt' });
    fireEvent.click(await screen.findByRole('button', { name: /Chat about these questions/ }));
    const box = screen.getByRole('textbox', { name: 'Message about these questions' });
    fireEvent.change(box, { target: { value: 'Why not Helm?' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatMessage).toHaveBeenCalledWith('S1', 'Why not Helm?', { round_chat: 'round:2' }));
  });

  it('a message typed while the mockup waits carries the settings you are looking at', async () => {
    vi.mocked(api.sendChatMessage).mockClear().mockResolvedValue({ ok: true, message_id: 'm1' });
    render(<ChatThread sessionID="S1" embedded />);
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    act(() => MockEventSource.instances[0].onopen?.());
    emit({ seq: 0, role: '', content: JSON.stringify({ type: 'snapshot', view: viewAtMockup() }), kind: 'blueprint' });
    await screen.findByRole('list', { name: 'Blueprint stages' });
    const opening = { layout: 'rows', group: 'off', tick: 'strike' };
    const flip = renderHook(() => useMockupSettings('S1', 2, opening, false));
    act(() => flip.result.current[1]('group', 'on'));
    const box = await screen.findByRole('textbox');
    fireEvent.change(box, { target: { value: 'Can you add tabs for different shops?' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() =>
      expect(api.sendChatMessage).toHaveBeenCalledWith('S1', 'Can you add tabs for different shops?', {
        mockup: {
          prompt_id: 'mockup:1',
          revision: 2,
          action: 'chat',
          settings: { layout: 'rows', group: 'on', tick: 'strike' },
        },
      }),
    );
  });

  it('passes the origin chat to the transcript', async () => {
    vi.mocked(api.getChat).mockResolvedValue({ ...session, origin_chat: 'C' });
    const origin: ChatSession = { ...session, id: 'C', title: 'Card budgets', kind: 'chat' };
    render(
      <MemoryRouter>
        <HandoffContext.Provider value={{ sessions: [origin], openHandoff: vi.fn() }}>
          <ChatThread sessionID="S1" embedded />
        </HandoffContext.Provider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    emit({ seq: 1, role: 'user', content: 'Cap card budgets', kind: 'handoff_need' });
    expect(await screen.findByText('Cap card budgets')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Card budgets' })).toHaveAttribute('href', '/chat/C');
  });
});
