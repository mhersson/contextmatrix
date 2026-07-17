import { render, screen, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { SESSION_EXPIRED_EVENT } from '../../api/client';
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
  it('none mode renders the app directly - the none-mode invariant', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    renderGate();

    await waitFor(() => expect(screen.getByText('THE APP')).toBeInTheDocument());
    expect(mocks.getAuthSession).not.toHaveBeenCalled();
    expect(screen.queryByText(/sign in/i)).not.toBeInTheDocument();
  });

  it('none mode + SSE CLOSED dispatch keeps the app rendered - no login bounce', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'none' });

    renderGate();
    await waitFor(() => expect(screen.getByText('THE APP')).toBeInTheDocument());

    // What useSSEBus dispatches when the server closes the stream for good
    // (readyState CLOSED) - in none mode e.g. a server restart, never a dead
    // session. The provider flips its internal status, but the gate only
    // bounces to the login page in multi mode.
    act(() => {
      window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
    });

    expect(screen.getByText('THE APP')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /sign in/i })).not.toBeInTheDocument();
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

  it('login page shows the deployment meta line and a password reveal toggle', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: '0.9.3', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });

    renderGate();

    await waitFor(() => expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument());
    expect(screen.getByText(/multi-user mode/)).toBeInTheDocument();
    expect(screen.getByText(/v0\.9\.3/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Show password' })).toBeInTheDocument();

    // The brand caption states product facts; fabricated live telemetry
    // ("N agents active") must not reappear, nor filler meta segments.
    expect(screen.getByText(/every change is a git commit/)).toBeInTheDocument();
    expect(screen.queryByText(/agents active/)).not.toBeInTheDocument();
    expect(screen.queryByText(/agents report via MCP/)).not.toBeInTheDocument();
  });

  it('redemption route renders even while anonymous', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });
    mocks.inspectToken.mockResolvedValue({ purpose: 'invite', username: 'carol' });

    renderGate('/auth/token/some-raw-token');

    await waitFor(() => expect(screen.getByText(/welcome, carol/i)).toBeInTheDocument());
    expect(screen.queryByText('THE APP')).not.toBeInTheDocument();

    // The regression that shipped: AuthGate renders the page by path
    // interception (no <Route>), so useParams() was empty and the page
    // inspected an EMPTY token. Pin the actual argument.
    expect(mocks.inspectToken).toHaveBeenCalledWith('some-raw-token');
  });

  it('redemption page receives tokens with URL-unsafe-looking prefixes intact', async () => {
    mocks.getAppConfig.mockResolvedValue({ theme: 'everforest', version: 'x', auth_mode: 'multi' });
    mocks.getAuthSession.mockRejectedValue({ code: 'UNAUTHORIZED', error: 'authentication required' });
    mocks.inspectToken.mockResolvedValue({ purpose: 'bootstrap', username: '' });

    // Real bootstrap tokens are base64url and can start with dashes.
    renderGate('/auth/token/--TVEiQ6qXzViM_lCYkOKH5noES3iyT9MXSX9R0qge4');

    await waitFor(() => expect(screen.getByText(/create the admin account/i)).toBeInTheDocument());
    expect(mocks.inspectToken).toHaveBeenCalledWith('--TVEiQ6qXzViM_lCYkOKH5noES3iyT9MXSX9R0qge4');
  });
});
