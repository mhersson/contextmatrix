import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { AuthProvider, useAuth } from './useAuth';
import { SESSION_EXPIRED_EVENT } from '../api/client';

const mocks = vi.hoisted(() => ({
  getAppConfig: vi.fn(),
  getAuthSession: vi.fn(),
  logout: vi.fn(),
}));

vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>();
  return {
    ...orig,
    api: { ...orig.api, getAppConfig: mocks.getAppConfig, getAuthSession: mocks.getAuthSession, logout: mocks.logout },
  };
});

function Probe() {
  const { mode, status, user } = useAuth();
  return <div>{`${mode}|${status}|${user?.username ?? '-'}`}</div>;
}

beforeEach(() => vi.resetAllMocks());

describe('AuthProvider', () => {
  it('none mode authenticates immediately without a session probe', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
    expect(mocks.getAuthSession).not.toHaveBeenCalled();
  });

  it('missing auth_mode behaves as none (old servers)', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
  });

  it('multi + live session → authenticated with user', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: 'Alice', is_admin: false });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('multi|authenticated|alice')).toBeInTheDocument());
  });

  it('multi + 401 → anonymous', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('multi|anonymous|-')).toBeInTheDocument());
  });

  it('session-expired event flips authenticated → anonymous', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: '', is_admin: false });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByText('multi|authenticated|alice')).toBeInTheDocument());

    act(() => { window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT)); });

    await waitFor(() => expect(screen.getByText('multi|anonymous|-')).toBeInTheDocument());
  });
});
