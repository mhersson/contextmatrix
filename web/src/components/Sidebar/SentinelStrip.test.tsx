import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { api } from '../../api/client';
import type { SentinelInfo, SentinelStatus } from '../../types';
import { SentinelProvider } from '../../hooks/useSentinel';
import { SentinelRailIcon, SentinelStrip } from './SentinelStrip';

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
    settings: { enabled: true, healing: true, friction: true, daily_cap_usd: 20, investigate_ceiling_usd: 2, headroom: 0.85 },
  };
}

const getMock = vi.mocked(api.getSentinel);

function renderStrip(ui = <SentinelStrip />) {
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

describe('SentinelStrip', () => {
  it('renders nothing while Sentinel is off', async () => {
    getMock.mockResolvedValue(info({ enabled: false, state: 'off', mode: '' }));
    const { container } = renderStrip();
    await vi.waitFor(() => expect(getMock).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('shows the state text and opens the popover', async () => {
    getMock.mockResolvedValue(info({ state: 'working', healing: 2 }));
    renderStrip();
    const strip = await screen.findByRole('button', { name: /Sentinel/ });
    expect(strip).toHaveTextContent('healing 2');
    expect(screen.queryByRole('group', { name: 'Sentinel' })).toBeNull();
    fireEvent.click(strip);
    expect(screen.getByRole('group', { name: 'Sentinel' })).toHaveTextContent('3 cards healed today');
  });

  it('shows the label and the full reason for a long pause reason', async () => {
    getMock.mockResolvedValue(info({ state: 'paused', reason: 'waiting for subscription headroom' }));
    renderStrip();
    const strip = await screen.findByRole('button', { name: /Sentinel/ });
    expect(strip).toHaveTextContent('Sentinel');
    expect(strip).toHaveTextContent('waiting for subscription headroom');
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
