import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import { api } from '../../api/client';
import type { BoardEvent, LLMChannelUsage, LLMChannelView } from '../../types';
import { LLMChannelRailDot, LLMChannelStrip } from './LLMChannelStrip';

vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, api: { getLLMChannel: vi.fn(), putLLMChannel: vi.fn() } };
});

const bus = vi.hoisted(() => {
  const handlers = new Map<string, (e: BoardEvent) => void>();
  return {
    handlers,
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      handlers.set(pattern, fn);
      return () => {
        if (handlers.get(pattern) === fn) handlers.delete(pattern);
      };
    },
  };
});
vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: bus.subscribe, connected: true, reconnectEpoch: 0 }),
}));

function emit(type: string) {
  const prefix = type.slice(0, type.indexOf('.'));
  bus.handlers.get(`${prefix}.*`)?.({ type, project: '', card_id: '', timestamp: '' } as BoardEvent);
}

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

function setAuth(mode: 'none' | 'multi', isAdmin = false) {
  authState.current = {
    mode,
    status: 'authenticated',
    user: mode === 'multi' ? { username: 'u', display_name: 'U', is_admin: isAdmin } : null,
    version: null,
    setUser: vi.fn(),
    logout: vi.fn(),
  };
}

const inAnHour = new Date(Date.now() + 3_600_000).toISOString();
const anHourAgo = new Date(Date.now() - 3_600_000).toISOString();
const threeMinAgo = new Date(Date.now() - 180_000).toISOString();

function usage(over: Partial<LLMChannelUsage> = {}): LLMChannelUsage {
  return { window: 'seven_day', utilization: 0.64, status: 'allowed', resets_at: inAnHour, seen_at: threeMinAgo, stale: false, ...over };
}

const zero = { cards: 0, chats: 0, blueprints: 0 };

function view(over: Partial<LLMChannelView> = {}): LLMChannelView {
  return {
    mode: 'claude_code',
    switchable: true,
    endpoint: { type: 'openrouter', label: 'OpenRouter' },
    claude_code: true,
    in_flight: { endpoint: zero, claude_code: zero },
    exceptions: [],
    usage: [usage()],
    ...over,
  };
}

const getMock = vi.mocked(api.getLLMChannel);
const putMock = vi.mocked(api.putLLMChannel);

const strip = () => screen.findByRole('button', { name: /LLM channel/ });
const popover = () => screen.getByRole('group', { name: 'LLM channel' });

async function openPopover() {
  const s = await strip();
  fireEvent.click(s);
  return popover();
}

beforeEach(() => {
  getMock.mockReset();
  putMock.mockReset();
  bus.handlers.clear();
  setAuth('none');
});

afterEach(() => {
  vi.useRealTimers();
});

describe('LLMChannelStrip', () => {
  it('renders nothing and stops refetching without claude_code', async () => {
    getMock.mockResolvedValue(view({ claude_code: false, switchable: false, mode: 'endpoint', usage: [] }));
    const { container } = render(<><LLMChannelStrip /><LLMChannelRailDot /></>);
    await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(container).toBeEmptyDOMElement();

    vi.useFakeTimers();
    act(() => {
      emit('worker.started');
      vi.advanceTimersByTime(60_000);
    });
    expect(getMock).toHaveBeenCalledTimes(2);
  });

  it('shows the mode and the most-used non-stale window', async () => {
    getMock.mockResolvedValue(view({
      usage: [usage({ window: 'five_hour', utilization: 0.97, stale: true }), usage({ window: 'seven_day', utilization: 0.64 })],
    }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveTextContent('Claude Code');
    expect(s).toHaveTextContent('64% wk');
    expect(s).not.toHaveTextContent('97%');
  });

  it('labels a five-hour window and marks the percentage warn from 80', async () => {
    getMock.mockResolvedValue(view({
      usage: [usage({ window: 'five_hour', utilization: 0.86 }), usage({ window: 'seven_day', utilization: 0.4 })],
    }));
    render(<LLMChannelStrip />);
    await strip();
    expect(screen.getByText('86% 5h')).toHaveClass('llm-strip__pct--warn');
  });

  it('reads "limit" and names the reset when the window is rejected', async () => {
    getMock.mockResolvedValue(view({ usage: [usage({ utilization: 1, status: 'rejected' })] }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(screen.getByText('limit')).toHaveClass('llm-strip__pct--stop');
    expect(s).toHaveAccessibleDescription(/Limit reached · resets /);

    const pop = await openPopover();
    expect(within(pop).getByText(/Runs park until the reset; nothing falls back to OpenRouter/)).toBeInTheDocument();
  });

  it('keeps the subscription reading off the strip in endpoint mode', async () => {
    getMock.mockResolvedValue(view({ mode: 'endpoint', usage: [usage({ utilization: 1, status: 'rejected' })] }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveTextContent('OpenRouter');
    expect(s).not.toHaveTextContent('%');
    expect(s).not.toHaveTextContent('limit');
    expect(s).toHaveAccessibleDescription('');

    const pop = await openPopover();
    expect(within(pop).queryByText(/Runs park/)).toBeNull();
  });

  it('labels the meters as Claude Code readings in endpoint mode', async () => {
    getMock.mockResolvedValue(view({ mode: 'endpoint', usage: [usage({ window: 'five_hour', utilization: 0.3 }), usage()] }));
    render(<LLMChannelStrip />);
    const pop = await openPopover();
    expect(within(pop).getByText('Claude Code 5-hour')).toBeInTheDocument();
    expect(within(pop).getByText('Claude Code weekly')).toBeInTheDocument();
    expect(within(pop).getByText('30%')).toBeInTheDocument();
    expect(within(pop).getByText('64%')).toBeInTheDocument();
  });

  it('omits the percentage without a usage reading', async () => {
    getMock.mockResolvedValue(view({ usage: [] }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).not.toHaveTextContent('%');
  });

  it('counts the other mode in flight on the sub-line and hides it at zero', async () => {
    getMock.mockResolvedValue(view({ in_flight: { endpoint: { cards: 1, chats: 1, blueprints: 0 }, claude_code: { cards: 3, chats: 0, blueprints: 0 } } }));
    const { unmount } = render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveAccessibleDescription('2 still on OpenRouter');

    const pop = await openPopover();
    expect(within(pop).getByText(/still on OpenRouter: 1 card run, 1 chat/)).toBeInTheDocument();
    unmount();

    getMock.mockResolvedValue(view());
    render(<LLMChannelStrip />);
    const quiet = await strip();
    expect(quiet).toHaveAccessibleDescription('');
    fireEvent.click(quiet);
    expect(within(popover()).queryByText(/still on/)).toBeNull();
  });

  it('names the architect exception only in claude_code mode', async () => {
    const exceptions = [{ backend: 'architect' as const, runs_on: 'endpoint' as const }];
    getMock.mockResolvedValue(view({ exceptions }));
    const { unmount } = render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveAccessibleDescription('Blueprints stay on OpenRouter');
    const pop = await openPopover();
    expect(within(pop).getByText(/Blueprints stay on OpenRouter: the architect/)).toBeInTheDocument();
    unmount();

    getMock.mockResolvedValue(view({ mode: 'endpoint', exceptions }));
    render(<LLMChannelStrip />);
    const ep = await strip();
    expect(ep).toHaveTextContent('OpenRouter');
    expect(ep).toHaveAccessibleDescription('');
    fireEvent.click(ep);
    expect(within(popover()).queryByText(/stay on/)).toBeNull();
  });

  it('opens the popover on a click and closes it on Escape, an outside click or a second click', async () => {
    getMock.mockResolvedValue(view());
    render(<><LLMChannelStrip /><button>elsewhere</button></>);
    const s = await strip();
    expect(s).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(s);
    expect(s).toHaveAttribute('aria-expanded', 'true');
    expect(s).toHaveAttribute('aria-controls', popover().id);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('group', { name: 'LLM channel' })).toBeNull();
    expect(s).toHaveFocus();

    fireEvent.click(s);
    fireEvent.mouseDown(screen.getByRole('button', { name: 'elsewhere' }));
    expect(screen.queryByRole('group', { name: 'LLM channel' })).toBeNull();

    fireEvent.click(s);
    fireEvent.mouseDown(s);
    fireEvent.click(s);
    expect(screen.queryByRole('group', { name: 'LLM channel' })).toBeNull();
  });

  it('lets an admin switch with the active side pressed and applies the returned view', async () => {
    setAuth('multi', true);
    getMock.mockResolvedValue(view());
    putMock.mockResolvedValue(view({ mode: 'endpoint' }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).not.toHaveClass('llm-strip--ro');

    const pop = await openPopover();
    expect(within(pop).getByRole('button', { name: 'Claude Code' })).toHaveAttribute('aria-pressed', 'true');
    const endpoint = within(pop).getByRole('button', { name: 'OpenRouter' });
    expect(endpoint).toHaveAttribute('aria-pressed', 'false');

    fireEvent.click(endpoint);
    expect(putMock).toHaveBeenCalledWith('endpoint');
    await waitFor(() => expect(within(pop).getByRole('button', { name: 'OpenRouter' })).toHaveAttribute('aria-pressed', 'true'));
    expect(s).toHaveTextContent('OpenRouter');
  });

  it('lets anyone switch in none mode', async () => {
    setAuth('none');
    getMock.mockResolvedValue(view());
    render(<LLMChannelStrip />);
    const pop = await openPopover();
    expect(within(pop).getAllByRole('button')).toHaveLength(2);
  });

  it('shows a failed switch inline and clears it when the popover closes', async () => {
    getMock.mockResolvedValue(view());
    putMock.mockRejectedValue({ error: 'llm channel mode is not configured', code: 'BAD_REQUEST' });
    render(<LLMChannelStrip />);
    const s = await strip();
    const pop = await openPopover();
    fireEvent.click(within(pop).getByRole('button', { name: 'OpenRouter' }));
    expect(await within(pop).findByText('llm channel mode is not configured')).toBeInTheDocument();
    expect(within(pop).getByRole('button', { name: 'Claude Code' })).toHaveAttribute('aria-pressed', 'true');

    fireEvent.keyDown(document, { key: 'Escape' });
    fireEvent.click(s);
    expect(within(popover()).queryByRole('alert')).toBeNull();

    fireEvent.click(within(popover()).getByRole('button', { name: 'OpenRouter' }));
    expect(await within(popover()).findByRole('alert')).toBeInTheDocument();
    fireEvent.click(s);
    fireEvent.click(s);
    expect(within(popover()).queryByRole('alert')).toBeNull();
  });

  it('shows a static row without a lock when only claude_code is configured', async () => {
    setAuth('multi', true);
    getMock.mockResolvedValue(view({ switchable: false, endpoint: null }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveClass('llm-strip--ro');

    const pop = await openPopover();
    expect(within(pop).queryAllByRole('button')).toHaveLength(0);
    expect(within(pop).getByText('Claude Code subscription')).toHaveAttribute('title', 'No endpoint is configured');
    expect(within(pop).queryByRole('img', { name: 'Locked' })).toBeNull();
  });

  it('gives a non-admin a dashed strip, no buttons and the lock', async () => {
    setAuth('multi', false);
    getMock.mockResolvedValue(view());
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).toHaveClass('llm-strip--ro');

    const pop = await openPopover();
    expect(within(pop).queryAllByRole('button')).toHaveLength(0);
    expect(within(pop).getByText('Claude Code subscription')).toHaveAttribute('title', 'Set by an admin');
    expect(within(pop).getByRole('img', { name: 'Locked' })).toBeInTheDocument();
  });

  it('shows a stale reading with no percentage', async () => {
    getMock.mockResolvedValue(view({ usage: [usage({ resets_at: anHourAgo, stale: true })] }));
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(s).not.toHaveTextContent('%');

    const pop = await openPopover();
    expect(within(pop).getByText('Weekly limit')).toBeInTheDocument();
    expect(within(pop).getByText(/^reset .+ · no reading since$/)).toBeInTheDocument();
    expect(within(pop).queryByText(/%/)).toBeNull();
  });

  it('shows a meter per window with its reset and when it was seen', async () => {
    getMock.mockResolvedValue(view({ usage: [usage({ window: 'five_hour', utilization: 1.12 }), usage()] }));
    render(<LLMChannelStrip />);
    const pop = await openPopover();
    expect(within(pop).getByText('5-hour limit')).toBeInTheDocument();
    expect(within(pop).getByText('112%')).toBeInTheDocument();
    expect(within(pop).getByText('Weekly limit')).toBeInTheDocument();
    expect(within(pop).getByText('64%')).toBeInTheDocument();
    expect(within(pop).getAllByText(/^resets .+ · seen 3m ago$/)).toHaveLength(2);
  });

  it('refetches once, debounced, on llm_channel and worker events', async () => {
    getMock.mockResolvedValue(view());
    render(<LLMChannelStrip />);
    const s = await strip();
    expect(getMock).toHaveBeenCalledTimes(1);

    getMock.mockResolvedValue(view({ mode: 'endpoint' }));
    vi.useFakeTimers();
    act(() => {
      emit('llm_channel.changed');
      emit('worker.started');
      vi.advanceTimersByTime(999);
    });
    expect(getMock).toHaveBeenCalledTimes(1);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    vi.useRealTimers();
    expect(getMock).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(s).toHaveTextContent('OpenRouter'));
  });
});

describe('LLMChannelRailDot', () => {
  it('shows the mode as a dot', async () => {
    getMock.mockResolvedValue(view({ mode: 'endpoint' }));
    render(<LLMChannelRailDot />);
    expect(await screen.findByRole('img', { name: 'LLM channel: OpenRouter' })).toBeInTheDocument();
  });
});
