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

  it('retries a network-shaped app-config failure and resolves once the retry succeeds', async () => {
    mocks.getAppConfig
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: '', is_admin: false });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('multi|authenticated|alice')).toBeInTheDocument());
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(2);
  });

  it('retries an infra-shaped UNKNOWN_ERROR app-config failure and resolves once the retry succeeds', async () => {
    mocks.getAppConfig
      .mockRejectedValueOnce({ error: 'Bad Gateway', code: 'UNKNOWN_ERROR' })
      .mockRejectedValueOnce({ error: 'Bad Gateway', code: 'UNKNOWN_ERROR' })
      .mockResolvedValueOnce({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(3);
  });

  it('falls back to none only after all 3 retry attempts are exhausted', async () => {
    mocks.getAppConfig.mockRejectedValue(new TypeError('Failed to fetch'));

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(3);
  });

  it('unmount mid-backoff cancels the retry loop — no stray request fires', async () => {
    mocks.getAppConfig.mockRejectedValue(new TypeError('Failed to fetch'));

    const { unmount } = render(<AuthProvider><Probe /></AuthProvider>);

    // Let the first attempt fail, then unmount inside the 100ms backoff.
    await waitFor(() => expect(mocks.getAppConfig).toHaveBeenCalledTimes(1));
    unmount();

    // Wait past every backoff window; the aborted loop must not fire again.
    await new Promise((resolve) => setTimeout(resolve, 500));
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(1);
  });

  it('does not retry a structured non-transient app-config error — falls back to none immediately', async () => {
    mocks.getAppConfig.mockRejectedValue({ error: 'nope', code: 'SOME_REAL_ERROR' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(1);
  });

  it('a clean success does not trigger any retry', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByText('none|authenticated|-')).toBeInTheDocument());
    expect(mocks.getAppConfig).toHaveBeenCalledTimes(1);
  });

  it('logout flips to anonymous even when the API call rejects, without throwing', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: '', is_admin: false });
    mocks.logout.mockRejectedValue(new Error('session already dead'));

    function LogoutProbe() {
      const { mode, status, user, logout } = useAuth();
      return (
        <div>
          <div>{`${mode}|${status}|${user?.username ?? '-'}`}</div>
          <button onClick={() => void logout()}>logout</button>
        </div>
      );
    }

    render(<AuthProvider><LogoutProbe /></AuthProvider>);
    await waitFor(() => expect(screen.getByText('multi|authenticated|alice')).toBeInTheDocument());

    await act(async () => {
      screen.getByText('logout').click();
      // Flush the rejected api.logout() promise before asserting.
      await Promise.resolve();
    });

    await waitFor(() => expect(screen.getByText('multi|anonymous|-')).toBeInTheDocument());
    expect(mocks.logout).toHaveBeenCalledTimes(1);
  });
});
