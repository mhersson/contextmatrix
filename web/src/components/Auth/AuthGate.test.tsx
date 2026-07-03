import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { AuthProvider } from '../../hooks/useAuth';
import { AuthGate } from './AuthGate';

const mocks = vi.hoisted(() => ({
  getAppConfig: vi.fn(),
  getAuthSession: vi.fn(),
  inspectToken: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      getAppConfig: mocks.getAppConfig,
      getAuthSession: mocks.getAuthSession,
      inspectToken: mocks.inspectToken,
    },
  };
});

function renderGate(path = '/') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <AuthGate>
          <div>THE APP</div>
        </AuthGate>
      </AuthProvider>
    </MemoryRouter>
  );
}

beforeEach(() => vi.resetAllMocks());

describe('AuthGate', () => {
  it('none mode renders the app directly — the none-mode invariant', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    renderGate();

    await waitFor(() => expect(screen.getByText('THE APP')).toBeInTheDocument());
    expect(mocks.getAuthSession).not.toHaveBeenCalled();
    expect(screen.queryByText(/sign in/i)).not.toBeInTheDocument();
  });

  it('multi + anonymous shows the login page, not the app', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });

    renderGate();

    await waitFor(() => expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument());
    expect(screen.queryByText('THE APP')).not.toBeInTheDocument();
  });

  it('multi + session renders the app', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockResolvedValue({ username: 'alice', display_name: '', is_admin: false });

    renderGate();

    await waitFor(() => expect(screen.getByText('THE APP')).toBeInTheDocument());
  });

  it('redemption route renders even while anonymous', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });
    mocks.inspectToken.mockResolvedValue({ purpose: 'invite', username: 'carol' });

    renderGate('/auth/token/some-raw-token');

    await waitFor(() => expect(screen.getByText(/welcome, carol/i)).toBeInTheDocument());
    expect(screen.queryByText('THE APP')).not.toBeInTheDocument();
  });
});
