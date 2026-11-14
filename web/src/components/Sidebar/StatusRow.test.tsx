import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { api } from '../../api/client';
import type { LLMChannelView, SentinelInfo } from '../../types';
import { SentinelProvider } from '../../hooks/useSentinel';
import { channelLabel, subLineItems, subLineText } from '../../lib/llmChannel';
import { chipText } from '../../lib/sentinel';
import { StatusRow } from './StatusRow';

vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, api: { getLLMChannel: vi.fn(), getSentinel: vi.fn() } };
});

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => undefined, connected: true, reconnectEpoch: 0 }),
}));

vi.mock('../../hooks/useAuth', () => ({ useOptionalAuth: () => null }));

const zero = { cards: 0, chats: 0, blueprints: 0 };

function channel(over: Partial<LLMChannelView> = {}): LLMChannelView {
  return {
    mode: 'claude_code',
    switchable: true,
    endpoint: { type: 'openrouter', label: 'OpenRouter' },
    claude_code: true,
    in_flight: { endpoint: zero, claude_code: zero },
    exceptions: [],
    usage: [],
    ...over,
  };
}

function sentinel(enabled: boolean): SentinelInfo {
  return {
    status: {
      enabled,
      state: enabled ? 'watching' : 'off',
      mode: enabled ? 'endpoint' : '',
      healing: 0,
      healed_today: 0,
      spent_today_usd: 0,
      daily_cap_usd: 20,
      headroom: 0.85,
    },
    settings: { enabled, healing: true, friction: true, daily_cap_usd: 20, investigate_ceiling_usd: 2, headroom: 0.85, analyse_every_run: false, analyse_ceiling_usd: 1 },
  };
}

const llmMock = vi.mocked(api.getLLMChannel);
const sentinelMock = vi.mocked(api.getSentinel);

function renderRow() {
  return render(
    <MemoryRouter>
      <SentinelProvider>
        <StatusRow />
      </SentinelProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  llmMock.mockReset();
  sentinelMock.mockReset();
});

describe('StatusRow', () => {
  it('shows both chips in one row, fetching the channel once', async () => {
    const v = channel();
    llmMock.mockResolvedValue(v);
    const s = sentinel(true);
    sentinelMock.mockResolvedValue(s);
    const { container } = renderRow();
    const llm = await screen.findByRole('button', { name: /LLM channel/ });
    const sent = await screen.findByRole('button', { name: /Sentinel/ });
    const row = container.querySelector('.sb-status-row');
    expect(row).toContainElement(llm);
    expect(row).toContainElement(sent);
    expect(llm).toHaveTextContent(channelLabel(v, v.mode));
    expect(sent).toHaveTextContent(chipText(s.status));
    expect(llmMock).toHaveBeenCalledTimes(1);
  });

  it('drops the Sentinel chip while Sentinel is off', async () => {
    llmMock.mockResolvedValue(channel());
    sentinelMock.mockResolvedValue(sentinel(false));
    renderRow();
    await screen.findByRole('button', { name: /LLM channel/ });
    await vi.waitFor(() => expect(sentinelMock).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: /Sentinel/ })).toBeNull();
  });

  it('renders nothing when neither chip shows', async () => {
    llmMock.mockResolvedValue(channel({ claude_code: false, switchable: false, mode: 'endpoint' }));
    sentinelMock.mockResolvedValue(sentinel(false));
    const { container } = renderRow();
    await vi.waitFor(() => expect(sentinelMock).toHaveBeenCalled());
    await vi.waitFor(() => expect(llmMock).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('puts the notice line under the row, carrying the same text the channel produces', async () => {
    const v = channel({ in_flight: { endpoint: { cards: 1, chats: 1, blueprints: 0 }, claude_code: zero }, mode: 'claude_code' });
    llmMock.mockResolvedValue(v);
    sentinelMock.mockResolvedValue(sentinel(false));
    const { container } = renderRow();
    const [item] = subLineItems(v);
    expect(item).toBeDefined();
    const line = await screen.findByText((_, el) => el?.tagName === 'P' && el.textContent === subLineText(item));
    const row = container.querySelector('.sb-status-row') as HTMLElement;
    expect(row.nextElementSibling).toBe(line);
  });

  it('shows no notice line when nothing needs saying', async () => {
    llmMock.mockResolvedValue(channel());
    sentinelMock.mockResolvedValue(sentinel(false));
    const { container } = renderRow();
    await screen.findByRole('button', { name: /LLM channel/ });
    expect(container.querySelector('p')).toBeNull();
  });
});
