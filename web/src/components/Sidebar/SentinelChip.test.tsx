import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { api } from '../../api/client';
import type { SentinelInfo, SentinelStatus } from '../../types';
import { SentinelProvider } from '../../hooks/useSentinel';
import { chipText, stripText } from '../../lib/sentinel';
import { SentinelChip, SentinelRailIcon } from './SentinelChip';

vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, api: { getSentinel: vi.fn() } };
});

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => undefined, connected: true, reconnectEpoch: 0 }),
}));

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

function setAuth(mode: 'none' | 'multi', isAdmin = false) {
  authState.current = {
    mode,
    status: 'authenticated',
    user: mode === 'multi' ? { username: 'u', display_name: 'U', is_admin: isAdmin } : null,
  };
}

function info(over: Partial<SentinelStatus> = {}): SentinelInfo {
  return {
    status: {
      enabled: true,
      state: 'watching',
      mode: 'endpoint',
      healing: 0,
      healed_today: 3,
      spent_today_usd: 1.2,
      daily_cap_usd: 20,
      headroom: 0.85,
      ...over,
    },
    settings: { enabled: true, healing: true, friction: true, daily_cap_usd: 20, investigate_ceiling_usd: 2, headroom: 0.85, analyse_every_run: false, analyse_ceiling_usd: 1 },
  };
}

const getMock = vi.mocked(api.getSentinel);

function renderStrip(ui = <SentinelChip />) {
  return render(
    <MemoryRouter>
      <SentinelProvider>{ui}</SentinelProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  getMock.mockReset();
  setAuth('none');
});

describe('SentinelChip', () => {
  it('renders nothing while Sentinel is off', async () => {
    getMock.mockResolvedValue(info({ enabled: false, state: 'off', mode: '' }));
    const { container } = renderStrip();
    await vi.waitFor(() => expect(getMock).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('shows the state word and opens the popover', async () => {
    const i = info({ state: 'working', healing: 2 });
    getMock.mockResolvedValue(i);
    renderStrip();
    const chip = await screen.findByRole('button', { name: /Sentinel/ });
    expect(chip).toHaveTextContent(chipText(i.status));
    expect(screen.queryByRole('group', { name: 'Sentinel' })).toBeNull();
    fireEvent.click(chip);
    expect(screen.getByRole('group', { name: 'Sentinel' })).toHaveTextContent('3 cards healed today');
  });

  it('says "paused" on the chip and keeps the reason in the tooltip, the name and the popover', async () => {
    const i = info({ state: 'paused', reason: 'waiting for subscription headroom' });
    getMock.mockResolvedValue(i);
    renderStrip();
    const chip = await screen.findByRole('button', { name: /Sentinel/ });
    expect(chipText(i.status)).toBe('paused');
    expect(chip).toHaveTextContent('paused');
    expect(chip).not.toHaveTextContent(i.status.reason as string);
    expect(chip).toHaveAttribute('title', stripText(i.status));
    expect(chip).toHaveAccessibleName(`Sentinel: ${stripText(i.status)}`);
    fireEvent.click(chip);
    expect(screen.getByRole('group', { name: 'Sentinel' })).toHaveTextContent(i.status.reason as string);
  });

  it('closes the popover on Escape', async () => {
    getMock.mockResolvedValue(info());
    renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('group', { name: 'Sentinel' })).toBeNull();
  });

  it('shows the settings link only to admins in multi mode', async () => {
    getMock.mockResolvedValue(info());
    setAuth('multi', false);
    const { unmount } = renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    expect(screen.queryByRole('link', { name: 'Sentinel settings' })).toBeNull();
    unmount();

    setAuth('multi', true);
    renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    expect(screen.getByRole('link', { name: 'Sentinel settings' })).toHaveAttribute('href', '/admin/sentinel');
  });

  it('shows the settings link in none mode', async () => {
    getMock.mockResolvedValue(info());
    renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    expect(screen.getByRole('link', { name: 'Sentinel settings' })).toBeInTheDocument();
  });

  it('shows the endpoint spend line or the Claude Code windows by mode', async () => {
    getMock.mockResolvedValue(info());
    const { unmount } = renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    expect(screen.getByText('$1.20 of $20.00 today')).toBeInTheDocument();
    unmount();

    getMock.mockResolvedValue(info({ mode: 'claude_code', windows: [{ window: 'five_hour', utilization: 0.62 }] }));
    renderStrip();
    fireEvent.click(await screen.findByRole('button', { name: /Sentinel/ }));
    expect(screen.getByText('five_hour 62% of 85%')).toBeInTheDocument();
    expect(screen.queryByText(/ of \$20\.00 today/)).toBeNull();
  });
});

describe('SentinelRailIcon', () => {
  it('names the state and renders nothing while off', async () => {
    getMock.mockResolvedValue(info({ state: 'paused', reason: 'daily cap reached' }));
    const { unmount } = renderStrip(<SentinelRailIcon />);
    expect(await screen.findByRole('img', { name: 'Sentinel: daily cap reached' })).toBeInTheDocument();
    unmount();

    getMock.mockResolvedValue(info({ enabled: false, state: 'off', mode: '' }));
    const { container } = renderStrip(<SentinelRailIcon />);
    await vi.waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
    expect(container).toBeEmptyDOMElement();
  });
});
