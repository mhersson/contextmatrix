import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../api/client';
import type { BoardEvent, ChatModelList } from '../types';
import { resetChatModelsCache, useChatModels, useChatModelsChannelReset, type ChatModelsKind } from './chatModels';

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  api: { listChatModels: vi.fn() },
}));

const bus = vi.hoisted(() => ({ handlers: new Map<string, (e: BoardEvent) => void>() }));
vi.mock('../hooks/useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      bus.handlers.set(pattern, fn);
      return () => {
        if (bus.handlers.get(pattern) === fn) bus.handlers.delete(pattern);
      };
    },
    connected: true,
    reconnectEpoch: 0,
  }),
}));

function emit(type: string) {
  const prefix = type.slice(0, type.indexOf('.'));
  act(() => bus.handlers.get(`${prefix}.*`)?.({ type, project: '', card_id: '', timestamp: '' } as BoardEvent));
}

let source: ChatModelList['source'] = 'endpoint';

function Models({ kind }: { kind: ChatModelsKind }) {
  const { models } = useChatModels(kind);
  return <p data-testid={kind}>{models.map((m) => m.id).join(',')}</p>;
}

function ChannelReset() {
  useChatModelsChannelReset();
  return null;
}

beforeEach(() => {
  resetChatModelsCache();
  bus.handlers.clear();
  source = 'endpoint';
  vi.mocked(api.listChatModels).mockReset().mockImplementation(async (kind?: 'architect') => {
    const id = source === 'claude_code' ? 'claude-sonnet-5-5' : `${kind ?? 'chat'}/endpoint-model`;
    return { source, default: id, architect_default: id, models: [{ id, label: id, max_tokens: 1000 }] };
  });
});

describe('chat model lists across an LLM channel switch', () => {
  it('serves the cached lists until an llm_channel event, then refetches both for the next consumer', async () => {
    render(<ChannelReset />);

    const first = render(<><Models kind="chat" /><Models kind="architect" /></>);
    await waitFor(() => expect(screen.getByTestId('chat')).toHaveTextContent('chat/endpoint-model'));
    await waitFor(() => expect(screen.getByTestId('architect')).toHaveTextContent('architect/endpoint-model'));
    first.unmount();

    source = 'claude_code';
    const cached = render(<Models kind="chat" />);
    expect(screen.getByTestId('chat')).toHaveTextContent('chat/endpoint-model');
    cached.unmount();
    expect(api.listChatModels).toHaveBeenCalledTimes(2);

    emit('llm_channel.changed');

    render(<><Models kind="chat" /><Models kind="architect" /></>);
    await waitFor(() => expect(screen.getByTestId('chat')).toHaveTextContent('claude-sonnet-5-5'));
    await waitFor(() => expect(screen.getByTestId('architect')).toHaveTextContent('claude-sonnet-5-5'));
    expect(api.listChatModels).toHaveBeenCalledTimes(4);
  });
});
