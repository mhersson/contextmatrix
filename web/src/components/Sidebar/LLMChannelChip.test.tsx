import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import { api } from '../../api/client';
import type { BoardEvent, LLMChannelUsage, LLMChannelView } from '../../types';
import { useLLMChannel } from '../../hooks/useLLMChannel';
import { LLMChannelChip, LLMChannelRailDot } from './LLMChannelChip';
import { channelLabel, chipTone, lockedRowTitle, usagePct, windowLabel } from '../../lib/llmChannel';

vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, api: { getLLMChannel: vi.fn(), putLLMChannel: vi.fn() } };
});

const bus = vi.hoisted(() => {
  const handlers = new Map<string, (e: BoardEvent) => void>();
  return {
    handlers,
    epoch: 0,
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      handlers.set(pattern, fn);
      return () => {
        if (handlers.get(pattern) === fn) handlers.delete(pattern);
      };
    },
  };
});
vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: bus.subscribe, connected: true, reconnectEpoch: bus.epoch }),
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

function Chip() {
  return <LLMChannelChip state={useLLMChannel()} />;
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
  bus.epoch = 0;
  setAuth('none');
});

afterEach(() => {
  vi.useRealTimers();
});

describe('LLMChannelChip', () => {
  it('renders nothing and stops refetching without claude_code', async () => {
    getMock.mockResolvedValue(view({ claude_code: false, switchable: false, mode: 'endpoint', usage: [] }));
    const { container } = render(<><Chip /><LLMChannelRailDot /></>);
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

  it('shows the channel name and no usage figure, keeping the most-used non-stale window in the label', async () => {
    const v = view({
      usage: [usage({ window: 'five_hour', utilization: 0.97, stale: true }), usage({ window: 'seven_day', utilization: 0.64 })],
    });
    getMock.mockResolvedValue(v);
    render(<Chip />);
    const s = await strip();
    expect(s).toHaveTextContent(channelLabel(v, v.mode));
    expect(s).not.toHaveTextContent('%');
    expect(s).toHaveAccessibleName(`LLM channel: ${channelLabel(v, v.mode)}, ${usagePct(v.usage[1])}% wk`);
    expect(s).toHaveClass(`sb-chip--${chipTone(v)}`);
  });

  it('turns warn from 80 percent and stop on a rejected window', async () => {
    const warn = view({ usage: [usage({ window: 'five_hour', utilization: 0.86 }), usage({ utilization: 0.4 })] });
    getMock.mockResolvedValue(warn);
    const { unmount } = render(<Chip />);
    const s = await strip();
    expect(chipTone(warn)).toBe('warn');
    expect(s).toHaveClass('sb-chip--warn');
    expect(s).toHaveAccessibleName(/86% 5h/);
    unmount();

    const stop = view({ usage: [usage({ utilization: 1, status: 'rejected' })] });
    getMock.mockResolvedValue(stop);
    render(<Chip />);
    const limit = await strip();
    expect(chipTone(stop)).toBe('stop');
    expect(limit).toHaveClass('sb-chip--stop');
    expect(limit).toHaveAccessibleName(/limit/);
    expect(limit).not.toHaveTextContent('%');

    fireEvent.click(limit);
    expect(popover().querySelector('.llm-line__icon--red')).not.toBeNull();
  });

  it('keeps the subscription reading off the chip in endpoint mode', async () => {
    const v = view({ mode: 'endpoint', usage: [usage({ utilization: 1, status: 'rejected' })] });
    getMock.mockResolvedValue(v);
    render(<Chip />);
    const s = await strip();
    expect(chipTone(v)).toBe('ok');
    expect(s).toHaveTextContent(channelLabel(v, 'endpoint'));
    expect(s).not.toHaveClass('sb-chip--stop');
    expect(s).not.toHaveAccessibleName(/limit|%/);
    expect(s.querySelector('.llm-dot--endpoint')).not.toBeNull();

    const pop = await openPopover();
    expect(pop.querySelector('.llm-line__icon--red')).toBeNull();
  });

  it('labels the meters as Claude Code readings in endpoint mode', async () => {
    getMock.mockResolvedValue(view({ mode: 'endpoint', usage: [usage({ window: 'five_hour', utilization: 0.3 }), usage()] }));
    render(<Chip />);
    const pop = await openPopover();
    expect(within(pop).getByText(windowLabel('five_hour', 'endpoint'))).toBeInTheDocument();
    expect(within(pop).getByText(windowLabel('seven_day', 'endpoint'))).toBeInTheDocument();
    expect(within(pop).getByText('30%')).toBeInTheDocument();
    expect(within(pop).getByText('64%')).toBeInTheDocument();
  });

  it('opens the popover on a click and closes it on Escape, an outside click or a second click', async () => {
    getMock.mockResolvedValue(view());
    render(<><Chip /><button>elsewhere</button></>);
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

  it('closes the popover when focus leaves it, without pulling focus back', async () => {
    getMock.mockResolvedValue(view());
    render(<><Chip /><button>elsewhere</button></>);
    const s = await strip();
    act(() => s.focus());
    fireEvent.click(s);
    act(() => within(popover()).getByRole('button', { name: 'OpenRouter' }).focus());
    act(() => s.focus());
    act(() => s.blur());
    expect(popover()).toBeInTheDocument();

    const elsewhere = screen.getByRole('button', { name: 'elsewhere' });
    act(() => within(popover()).getByRole('button', { name: 'OpenRouter' }).focus());
    act(() => elsewhere.focus());
    expect(screen.queryByRole('group', { name: 'LLM channel' })).toBeNull();
    expect(elsewhere).toHaveFocus();
  });

  it('lets an admin switch with the active side pressed and applies the returned view', async () => {
    setAuth('multi', true);
    getMock.mockResolvedValue(view());
    putMock.mockResolvedValue(view({ mode: 'endpoint' }));
    render(<Chip />);
    const s = await strip();
    expect(s).not.toHaveClass('sb-chip--ro');

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
    render(<Chip />);
    const pop = await openPopover();
    expect(within(pop).getAllByRole('button')).toHaveLength(2);
  });

  it('shows a failed switch inline and clears it when the popover closes', async () => {
    getMock.mockResolvedValue(view());
    putMock.mockRejectedValue({ error: 'llm channel mode is not configured', code: 'BAD_REQUEST' });
    render(<Chip />);
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
    const v = view({ switchable: false, endpoint: null });
    getMock.mockResolvedValue(v);
    render(<Chip />);
    const s = await strip();
    expect(s).toHaveClass('sb-chip--ro');

    const pop = await openPopover();
    expect(within(pop).queryAllByRole('button')).toHaveLength(0);
    expect(pop.querySelector('.llm-row')).toHaveAttribute('title', lockedRowTitle(v));
    expect(within(pop).queryByRole('img', { name: 'Locked' })).toBeNull();
  });

  it('gives a non-admin a dashed strip, no buttons and the lock', async () => {
    setAuth('multi', false);
    const v = view();
    getMock.mockResolvedValue(v);
    render(<Chip />);
    const s = await strip();
    expect(s).toHaveClass('sb-chip--ro');

    const pop = await openPopover();
    expect(within(pop).queryAllByRole('button')).toHaveLength(0);
    expect(pop.querySelector('.llm-row')).toHaveAttribute('title', lockedRowTitle(v));
    expect(lockedRowTitle(v)).not.toBe(lockedRowTitle(view({ switchable: false })));
    expect(within(pop).getByRole('img', { name: 'Locked' })).toBeInTheDocument();
  });

  it('shows a stale reading with no percentage', async () => {
    getMock.mockResolvedValue(view({ usage: [usage({ resets_at: anHourAgo, stale: true })] }));
    render(<Chip />);
    const s = await strip();
    expect(s).not.toHaveTextContent('%');

    const pop = await openPopover();
    expect(within(pop).getByText(windowLabel('seven_day', 'claude_code'))).toBeInTheDocument();
    expect(pop.querySelector('.llm-meter__pct')).toBeNull();
    expect(within(pop).queryByText(/%/)).toBeNull();
  });

  it('shows a meter per window with its reset and when it was seen', async () => {
    getMock.mockResolvedValue(view({ usage: [usage({ window: 'five_hour', utilization: 1.12 }), usage()] }));
    render(<Chip />);
    const pop = await openPopover();
    expect(within(pop).getByText(windowLabel('five_hour', 'claude_code'))).toBeInTheDocument();
    expect(within(pop).getByText('112%')).toBeInTheDocument();
    expect(within(pop).getByText(windowLabel('seven_day', 'claude_code'))).toBeInTheDocument();
    expect(within(pop).getByText('64%')).toBeInTheDocument();
    expect(pop.querySelectorAll('.llm-meter')).toHaveLength(2);
  });

  it('refetches once, debounced, on llm_channel and worker events', async () => {
    getMock.mockResolvedValue(view());
    render(<Chip />);
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
  it('refetches after an SSE reconnect, not on a plain rerender', async () => {
    getMock.mockResolvedValue(view());
    const { rerender } = render(<Chip />);
    await strip();
    rerender(<Chip />);
    await act(async () => {});
    expect(getMock).toHaveBeenCalledTimes(1);

    bus.epoch = 1;
    rerender(<Chip />);
    await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
  });
});

describe('LLMChannelRailDot', () => {
  it('shows the mode as a dot', async () => {
    getMock.mockResolvedValue(view({ mode: 'endpoint' }));
    render(<LLMChannelRailDot />);
    expect(await screen.findByRole('img', { name: 'LLM channel: OpenRouter' })).toBeInTheDocument();
  });
});
