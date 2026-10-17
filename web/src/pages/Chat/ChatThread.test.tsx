import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { api } from '../../api/client';
import { useChatStream } from '../../hooks/useChatStream';
import type { ChatSession, LogEntry } from '../../types';
import { ChatThread } from './ChatThread';

vi.mock('../../api/client', () => ({
  api: { getChat: vi.fn(), sendChatMessage: vi.fn() },
  isAPIError: vi.fn(() => false),
}));
vi.mock('../../hooks/useChatStream', () => ({ useChatStream: vi.fn() }));

const session: ChatSession = {
  id: 'bp-1',
  title: 'Blueprint',
  kind: 'architect',
  status: 'active',
  created_at: '2026-05-13T10:00:00Z',
  last_active: '2026-05-13T10:00:00Z',
  created_by: 'human:web',
};

const promptRow: LogEntry = {
  ts: '2026-05-13T10:00:00Z',
  card_id: '',
  type: 'text',
  kind: 'prompt',
  seq: 1,
  content: JSON.stringify({
    text: 'Sign off v2?',
    prompt: { id: 'gate:sign-off:v2', actions: [{ label: 'Approve', reply: 'approve' }] },
  }),
};

function streamState(connected: boolean) {
  vi.mocked(useChatStream).mockReturnValue({
    logs: [promptRow],
    connected,
    sessionUpdate: null,
    hasMore: false,
    loadingOlder: false,
    loadOlder: vi.fn(),
  } as unknown as ReturnType<typeof useChatStream>);
}

describe('ChatThread prompt buttons', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.getChat).mockResolvedValue(session);
  });

  it('disables the open prompt while the stream is disconnected', async () => {
    streamState(false);
    render(<ChatThread sessionID="bp-1" embedded />);

    expect(await screen.findByRole('button', { name: 'Approve' })).toBeDisabled();
  });

  it('enables the open prompt while the stream is connected', async () => {
    streamState(true);
    render(<ChatThread sessionID="bp-1" embedded />);

    expect(await screen.findByRole('button', { name: 'Approve' })).toBeEnabled();
  });

  it('disables the open prompt while the session is not taking sends', async () => {
    vi.mocked(api.getChat).mockResolvedValue({ ...session, status: 'cold' });
    streamState(true);
    render(<ChatThread sessionID="bp-1" embedded />);

    expect(await screen.findByRole('button', { name: 'Approve' })).toBeDisabled();
  });
});
