import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { api } from '../../api/client';
import { useChatStream } from '../../hooks/useChatStream';
import type { ChatSession, LogEntry } from '../../types';
import { ChatThread } from './ChatThread';

vi.mock('../../api/client', () => ({
  api: { getChat: vi.fn(), sendChatMessage: vi.fn() },
  isAPIError: vi.fn(() => false),
}));
vi.mock('../../hooks/useChatStream', () => ({ useChatStream: vi.fn() }));
// The real pane fetches the blueprint view; ChatThread.blueprint.test.tsx covers it.
vi.mock('../../components/Blueprint/BlueprintSession', () => ({
  BlueprintSession: ({ phone, onReply, children }: { phone: boolean; onReply: (text: string) => Promise<void>; children: ReactNode }) => (
    <section aria-label={phone ? 'Blueprint pane, phone layout' : 'Blueprint pane'}>
      {children}
      <button type="button" onClick={() => onReply('approve').catch(() => undefined)}>
        Gate reply
      </button>
    </section>
  ),
}));

// Chat entries lazy-load the markdown chunk; loading it up front keeps the
// import from finishing after this file's environment is torn down.
beforeAll(() => import('../../components/Markdown/SafeMarkdownPreview'));

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

describe('ChatThread blueprint pane', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    streamState(true);
    vi.mocked(api.sendChatMessage).mockResolvedValue({ ok: true, message_id: 'm1' });
  });

  it('wraps an architect session in the blueprint pane with the transcript reply path', async () => {
    vi.mocked(api.getChat).mockResolvedValue(session);
    render(<ChatThread sessionID="bp-1" embedded phone />);

    expect(await screen.findByRole('region', { name: 'Blueprint pane, phone layout' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Gate reply' }));
    await waitFor(() => expect(api.sendChatMessage).toHaveBeenCalledWith('bp-1', 'approve', undefined));
  });

  it('leaves a plain chat as a transcript', async () => {
    vi.mocked(api.getChat).mockResolvedValue({ ...session, kind: 'chat' });
    render(<ChatThread sessionID="bp-1" embedded />);

    await screen.findByRole('button', { name: 'Approve' });
    expect(screen.queryByRole('region', { name: /Blueprint pane/ })).not.toBeInTheDocument();
  });

  it('shows the working indicator while the send is in flight and drops it when the send fails', async () => {
    vi.mocked(api.getChat).mockResolvedValue(session);
    let reject!: (e: Error) => void;
    vi.mocked(api.sendChatMessage).mockReturnValue(new Promise((_, r) => { reject = r; }));
    render(<ChatThread sessionID="bp-1" embedded />);

    fireEvent.click(await screen.findByRole('button', { name: 'Gate reply' }));
    expect(await screen.findByTestId('working-indicator')).toBeInTheDocument();

    reject(new Error('boom'));
    await waitFor(() => expect(screen.queryByTestId('working-indicator')).not.toBeInTheDocument());
  });
});
