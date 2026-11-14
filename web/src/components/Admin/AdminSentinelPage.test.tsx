import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { AdminSentinelPage } from './AdminSentinelPage';
import type { SentinelInfo, SentinelSettings } from '../../types';

const mocks = vi.hoisted(() => ({
  getSentinel: vi.fn(),
  putSentinelSettings: vi.fn(),
  refresh: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: { ...orig.api, getSentinel: mocks.getSentinel, putSentinelSettings: mocks.putSentinelSettings },
  };
});

vi.mock('../../hooks/useSentinel', () => ({
  useSentinel: () => ({ info: null, refresh: mocks.refresh }),
}));

const settings: SentinelSettings = {
  enabled: true,
  healing: true,
  friction: true,
  daily_cap_usd: 20,
  investigate_ceiling_usd: 2,
  headroom: 0.85,
};

function info(over: Partial<SentinelSettings> = {}, extra: Partial<SentinelInfo> = {}): SentinelInfo {
  return {
    status: { enabled: true, state: 'watching', mode: 'endpoint', healed_today: 0 } as SentinelInfo['status'],
    settings: { ...settings, ...over },
    ...extra,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getSentinel.mockResolvedValue(info());
  mocks.putSentinelSettings.mockImplementation(async (s: SentinelSettings) => info({ ...s }));
});

describe('AdminSentinelPage', () => {
  it('loads the settings and shows the threshold as a percent', async () => {
    render(<AdminSentinelPage />);
    expect(await screen.findByLabelText('Subscription headroom threshold (percent)')).toHaveValue(85);
    expect(screen.getByLabelText('Daily spend cap (USD)')).toHaveValue(20);
    expect(screen.getByLabelText('Investigation ceiling (USD per investigation)')).toHaveValue(2);
  });

  it('sends headroom as a fraction and refreshes the strip', async () => {
    render(<AdminSentinelPage />);
    const field = await screen.findByLabelText('Subscription headroom threshold (percent)');
    fireEvent.change(field, { target: { value: '70' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(mocks.putSentinelSettings).toHaveBeenCalled());
    expect(mocks.putSentinelSettings.mock.calls[0][0]).toEqual({ ...settings, headroom: 0.7 });
    await waitFor(() => expect(mocks.refresh).toHaveBeenCalled());
  });

  it('disables Healing and Friction while Sentinel is off', async () => {
    render(<AdminSentinelPage />);
    const sentinel = await screen.findByRole('switch', { name: 'Sentinel' });
    expect(screen.getByRole('switch', { name: 'Healing' })).toBeEnabled();
    fireEvent.click(sentinel);
    expect(screen.getByRole('switch', { name: 'Healing' })).toBeDisabled();
    expect(screen.getByRole('switch', { name: 'Friction analysis' })).toBeDisabled();
  });

  it('shows the validation error from a 422', async () => {
    mocks.putSentinelSettings.mockRejectedValue({ error: 'validation failed', code: 'VALIDATION_ERROR', details: 'daily_cap_usd out of range' });
    render(<AdminSentinelPage />);
    await screen.findByRole('switch', { name: 'Sentinel' });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('daily_cap_usd out of range');
    expect(screen.getByLabelText('Daily spend cap (USD)')).toHaveValue(20);
    expect(mocks.refresh).not.toHaveBeenCalled();
  });

  it('blocks the save and shows an error when a number field is empty', async () => {
    render(<AdminSentinelPage />);
    fireEvent.change(await screen.findByLabelText('Daily spend cap (USD)'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toBeInTheDocument();
    expect(mocks.putSentinelSettings).not.toHaveBeenCalled();
  });

  it('shows who last updated the settings', async () => {
    mocks.getSentinel.mockResolvedValue(info({}, { updated_by: 'human:alice', updated_at: '2026-11-13T10:00:00Z' }));
    render(<AdminSentinelPage />);
    expect(await screen.findByText(/Updated by human:alice at/)).toBeInTheDocument();
  });
});
