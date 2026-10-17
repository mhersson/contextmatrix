import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import type { ChatSession } from '../../types';
import { ChatThread } from './ChatThread';

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
    MockEventSource.instances = [];
    vi.stubGlobal('EventSource', MockEventSource);
    vi.mocked(api.getChat).mockResolvedValue(session);
    vi.mocked(api.listChatMessagesTail).mockResolvedValue({ messages: [] });
    // The view fetch never answers, so only the stream can draw the pane.
    vi.mocked(api.getBlueprintView).mockReturnValue(new Promise(() => {}));
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
});
