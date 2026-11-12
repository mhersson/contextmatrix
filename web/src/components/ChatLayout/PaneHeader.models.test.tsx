import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { clearChatLiveData, setChatLiveData } from '../../hooks/useChatLiveData';
import { resetChatModelsCache } from '../../utils/chatModels';
import { PaneHeader } from './PaneHeader';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  api: { listChatModels: vi.fn() },
}));

const props = {
  slot: 'TL' as const,
  draggable: false,
  isFocused: false,
  showSplit: false,
  showClose: false,
  onClose: () => {},
  onSplit: () => {},
};

// Chats run on Claude Code while the architect stays on the endpoint.
beforeEach(() => {
  resetChatModelsCache();
  vi.mocked(api.listChatModels).mockReset().mockImplementation(async (kind?: string) =>
    kind === 'architect'
      ? {
          source: 'endpoint',
          default: 'arch/model',
          architect_default: 'arch/model',
          models: [{ id: 'arch/model', label: 'Arch model', max_tokens: 200_000 }],
        }
      : {
          source: 'claude_code',
          default: 'claude-sonnet-5-5',
          architect_default: 'arch/model',
          models: [{ id: 'claude-sonnet-5-5', label: 'claude-sonnet-5-5', max_tokens: 1_000_000 }],
        },
  );
});

afterEach(() => {
  clearChatLiveData('bp-1');
  clearChatLiveData('chat-1');
});

describe('PaneHeader model lists in a mixed LLM channel', () => {
  it("reads a blueprint's context window from the architect's own list", async () => {
    act(() => setChatLiveData('bp-1', { model: 'arch/model', contextTokens: 50_000 }));
    render(<PaneHeader {...props} chatId="bp-1" chat={{ id: 'bp-1', title: 'Blueprint', kind: 'architect' }} />);
    expect(await screen.findByText('25%')).toHaveAttribute('title', 'Context: 50,000 / 200,000 tokens (25%)');
    expect(screen.getByText('Arch model')).toBeInTheDocument();
    expect(api.listChatModels).toHaveBeenCalledWith('architect');
  });

  it('gives a chat pane and a blueprint pane open together their own lists', async () => {
    act(() => {
      setChatLiveData('chat-1', { model: 'claude-sonnet-5-5', contextTokens: 100_000 });
      setChatLiveData('bp-1', { model: 'arch/model', contextTokens: 50_000 });
    });
    render(
      <>
        <PaneHeader {...props} chatId="chat-1" chat={{ id: 'chat-1', title: 'Chat', kind: 'chat' }} />
        <PaneHeader {...props} chatId="bp-1" chat={{ id: 'bp-1', title: 'Blueprint', kind: 'architect' }} />
      </>,
    );
    expect(await screen.findByText('10%')).toBeInTheDocument();
    expect(await screen.findByText('25%')).toBeInTheDocument();
  });

  it('keeps a chat on the chat list', async () => {
    act(() => setChatLiveData('chat-1', { model: 'claude-sonnet-5-5', contextTokens: 100_000 }));
    render(<PaneHeader {...props} chatId="chat-1" chat={{ id: 'chat-1', title: 'Chat', kind: 'chat' }} />);
    expect(await screen.findByText('10%')).toBeInTheDocument();
    await waitFor(() => expect(api.listChatModels).toHaveBeenCalled());
    expect(api.listChatModels).not.toHaveBeenCalledWith('architect');
  });
});
