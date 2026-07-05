import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { AuthProvider, useAuth } from '../../hooks/useAuth';
import { AdminGuard } from './AdminGuard';

const mocks = vi.hoisted(() => ({
  getAppConfig: vi.fn(),
  getAuthSession: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: { ...orig.api, getAppConfig: mocks.getAppConfig, getAuthSession: mocks.getAuthSession },
  };
});

// Simulates the mechanism by which a live session is refreshed with new
// server-side state (the same `setUser` call LoginPage / TokenRedemptionPage
// use). Exercises whether AdminGuard reacts to the *existing* AuthProvider
// tree updating, as opposed to only reading a value captured at mount.
function Promoter() {
  const { setUser } = useAuth();
  return (
    <button onClick={() => setUser({ username: 'alice', display_name: 'Alice', is_admin: true })}>
      promote
    </button>
  );
}

// The demotion direction of Promoter: same setUser mechanism, admin bit off.
function Demoter() {
  const { setUser } = useAuth();
  return (
    <button onClick={() => setUser({ username: 'alice', display_name: 'Alice', is_admin: false })}>
      demote
    </button>
  );
}

beforeEach(() => vi.resetAllMocks());

describe('AdminGuard — none mode', () => {
  it('passes non-admin sessions through in none mode — no admin role exists', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    render(
      <MemoryRouter>
        <AuthProvider>
          <AdminGuard>
            <div>secret admin content</div>
          </AdminGuard>
        </AuthProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText('secret admin content')).toBeInTheDocument());
    expect(screen.queryByText('Page not found')).not.toBeInTheDocument();
    // None mode never probes a session — getAuthSession must not be called.
    expect(mocks.getAuthSession).not.toHaveBeenCalled();
  });
});

describe('AdminGuard — live is_admin read', () => {
  it('hides guarded content for a non-admin session', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: 'Alice', is_admin: false });

    render(
      <MemoryRouter>
        <AuthProvider>
          <AdminGuard>
            <div>secret admin content</div>
          </AdminGuard>
        </AuthProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText('Page not found')).toBeInTheDocument());
    expect(screen.queryByText('secret admin content')).not.toBeInTheDocument();
  });

  it('reflects a mid-session promotion to admin without remounting — no reload required', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: 'Alice', is_admin: false });

    render(
      <MemoryRouter>
        <AuthProvider>
          <Promoter />
          <AdminGuard>
            <div>secret admin content</div>
          </AdminGuard>
        </AuthProvider>
      </MemoryRouter>
    );

    // Starts out non-admin: guarded content is hidden behind NotFound.
    await waitFor(() => expect(screen.getByText('Page not found')).toBeInTheDocument());
    expect(screen.queryByText('secret admin content')).not.toBeInTheDocument();

    // Same mounted tree, no remount: simulate the session picking up a
    // server-side promotion to admin.
    act(() => {
      screen.getByText('promote').click();
    });

    // AdminGuard reads useAuth().user?.is_admin directly in its render body
    // (no useState initializer / no-deps memo capturing a stale snapshot),
    // so this must flip immediately.
    await waitFor(() => expect(screen.getByText('secret admin content')).toBeInTheDocument());
    expect(screen.queryByText('Page not found')).not.toBeInTheDocument();
  });

  it('reflects a mid-session demotion to non-admin without remounting — content disappears', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: 'Alice', is_admin: true });

    render(
      <MemoryRouter>
        <AuthProvider>
          <Demoter />
          <AdminGuard>
            <div>secret admin content</div>
          </AdminGuard>
        </AuthProvider>
      </MemoryRouter>
    );

    // Starts out admin: guarded content is visible.
    await waitFor(() => expect(screen.getByText('secret admin content')).toBeInTheDocument());

    // Same mounted tree, no remount: simulate the session picking up a
    // server-side demotion (admin bit revoked mid-session).
    act(() => {
      screen.getByText('demote').click();
    });

    await waitFor(() => expect(screen.getByText('Page not found')).toBeInTheDocument());
    expect(screen.queryByText('secret admin content')).not.toBeInTheDocument();
  });
});
