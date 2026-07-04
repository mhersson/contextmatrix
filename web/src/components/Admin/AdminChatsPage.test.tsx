import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { AdminChatsPage } from './AdminChatsPage';
import type { ChatSession } from '../../types';

const mocks = vi.hoisted(() => ({
  adminListChats: vi.fn(),
  adminEndChat: vi.fn(),
  adminDeleteChat: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminListChats: mocks.adminListChats,
      adminEndChat: mocks.adminEndChat,
      adminDeleteChat: mocks.adminDeleteChat,
    },
  };
});

function sess(overrides: Partial<ChatSession> = {}): ChatSession {
  return {
    id: 'S1',
    title: 'triage',
    status: 'cold',
    created_at: '2026-07-01T10:00:00Z',
    last_active: '2026-07-02T10:00:00Z',
    created_by: 'human:alice',
    ...overrides,
  };
}

beforeEach(() => {
  vi.resetAllMocks();
});

describe('AdminChatsPage', () => {
  it('lists all chats with owner column, including legacy owners', async () => {
    mocks.adminListChats.mockResolvedValue([
      sess({ id: 'S1', title: 'triage', created_by: 'human:alice', status: 'active', estimated_cost_usd: 1.23 }),
      sess({ id: 'S2', title: 'old chat', created_by: 'human:web-1a2b3c4d' }),
    ]);

    render(<AdminChatsPage />);

    await waitFor(() => expect(screen.getByText('triage')).toBeInTheDocument());
    expect(screen.getByText('old chat')).toBeInTheDocument();
    expect(screen.getByText('human:alice')).toBeInTheDocument();
    expect(screen.getByText('human:web-1a2b3c4d')).toBeInTheDocument();
    expect(screen.getByText('$1.23')).toBeInTheDocument();
  });

  it('offers End only for non-cold sessions and calls adminEndChat', async () => {
    mocks.adminListChats.mockResolvedValue([
      sess({ id: 'S1', title: 'hot', status: 'active' }),
      sess({ id: 'S2', title: 'cold one', status: 'cold' }),
    ]);
    mocks.adminEndChat.mockResolvedValue(sess({ id: 'S1', status: 'ending' }));

    render(<AdminChatsPage />);

    await waitFor(() => expect(screen.getByText('hot')).toBeInTheDocument());
    const endButtons = screen.getAllByRole('button', { name: 'End' });
    expect(endButtons).toHaveLength(1);

    fireEvent.click(endButtons[0]);
    await waitFor(() => expect(mocks.adminEndChat).toHaveBeenCalledWith('S1'));
  });

  it('deletes only after the danger confirm modal', async () => {
    mocks.adminListChats.mockResolvedValue([sess({ id: 'S1', title: 'doomed' })]);
    mocks.adminDeleteChat.mockResolvedValue(undefined);

    render(<AdminChatsPage />);

    await waitFor(() => expect(screen.getByText('doomed')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Delete chat?')).toBeInTheDocument();
    expect(mocks.adminDeleteChat).not.toHaveBeenCalled();

    const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]); // modal confirm
    await waitFor(() => expect(mocks.adminDeleteChat).toHaveBeenCalledWith('S1'));
  });

  it('never renders a transcript link or message content', async () => {
    mocks.adminListChats.mockResolvedValue([sess({ id: 'S1', title: 'quiet' })]);

    render(<AdminChatsPage />);

    await waitFor(() => expect(screen.getByText('quiet')).toBeInTheDocument());
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });
});
