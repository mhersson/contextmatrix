import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { AdminCredentialsPage } from './AdminCredentialsPage';
import type { CredentialInfo } from '../../types';

const mocks = vi.hoisted(() => ({
  adminListCredentials: vi.fn(),
  adminCreateCredential: vi.fn(),
  adminUpdateCredential: vi.fn(),
  adminDeleteCredential: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminListCredentials: mocks.adminListCredentials,
      adminCreateCredential: mocks.adminCreateCredential,
      adminUpdateCredential: mocks.adminUpdateCredential,
      adminDeleteCredential: mocks.adminDeleteCredential,
    },
  };
});

function credential(overrides: Partial<CredentialInfo> = {}): CredentialInfo {
  return {
    name: 'github-pat',
    kind: 'pat',
    host: '',
    api_base_url: '',
    app_id: 0,
    installation_id: 0,
    created_by: 'human:alice',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

const LEAKED_SECRET = 'ghp_SUPERSECRETVALUE_shouldneverrender';

beforeEach(() => {
  vi.resetAllMocks();
});

describe('AdminCredentialsPage - list', () => {
  it('renders credential metadata and never renders a secret value', async () => {
    mocks.adminListCredentials.mockResolvedValue([
      credential({
        name: 'github-pat',
        kind: 'pat',
        host: 'github.example.com',
        created_by: 'human:alice',
        last_used_at: '2026-06-01T12:00:00Z',
        // Simulates a server bug leaking a secret-shaped field. The UI must
        // never surface it even if present on the wire object.
        ...({ secret: LEAKED_SECRET } as unknown as Partial<CredentialInfo>),
      }),
      credential({
        name: 'github-app',
        kind: 'app',
        app_id: 12345,
        installation_id: 67890,
        created_by: 'human:bob',
        disabled: true,
      }),
    ]);

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText('github-pat')).toBeInTheDocument());
    expect(screen.getByText('github-app')).toBeInTheDocument();
    expect(screen.getByText('github.example.com')).toBeInTheDocument();
    expect(screen.getByText('human:alice')).toBeInTheDocument();
    expect(screen.getByText('human:bob')).toBeInTheDocument();
    expect(screen.getByText('12345')).toBeInTheDocument();
    expect(screen.getByText('67890')).toBeInTheDocument();
    expect(screen.getByText(/disabled/i)).toBeInTheDocument();

    // No element anywhere in the rendered tree ever shows a secret value.
    expect(screen.queryByText(LEAKED_SECRET)).not.toBeInTheDocument();
    expect(document.body.textContent).not.toContain(LEAKED_SECRET);
  });

  it('renders friendly kind labels ("PAT" / "GitHub App") rather than raw kind values', async () => {
    mocks.adminListCredentials.mockResolvedValue([
      credential({ name: 'github-pat', kind: 'pat' }),
      credential({ name: 'github-app', kind: 'app', app_id: 1, installation_id: 2 }),
    ]);

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText('github-pat')).toBeInTheDocument());
    expect(screen.getByText('PAT')).toBeInTheDocument();
    expect(screen.getByText('GitHub App')).toBeInTheDocument();
    // Raw kind strings must never leak as their own table cell text.
    expect(screen.queryByText('pat')).not.toBeInTheDocument();
    expect(screen.queryByText('app')).not.toBeInTheDocument();
  });

  it('falls back to a generic message when adminListCredentials rejects with a non-APIError shape', async () => {
    // Malformed rejection: `.error` exists but isn't a string, so it must
    // fail the isAPIError guard and fall back rather than leak through.
    mocks.adminListCredentials.mockRejectedValue({ error: 12345 });

    render(<AdminCredentialsPage />);

    expect(await screen.findByText('Failed to load credentials.')).toBeInTheDocument();
    expect(screen.queryByText('12345')).not.toBeInTheDocument();
  });
});

describe('AdminCredentialsPage - create flow', () => {
  it('posts a PAT credential through adminCreateCredential', async () => {
    mocks.adminListCredentials.mockResolvedValueOnce([]).mockResolvedValueOnce([
      credential({ name: 'new-pat', kind: 'pat' }),
    ]);
    mocks.adminCreateCredential.mockResolvedValue(credential({ name: 'new-pat', kind: 'pat' }));

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText(/no credentials/i)).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: /add credential/i }));

    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText(/^name$/i), { target: { value: 'new-pat' } });
    fireEvent.change(within(dialog).getByLabelText(/secret/i), { target: { value: 'ghp_freshtoken' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /add credential|save|create/i }));

    await waitFor(() =>
      expect(mocks.adminCreateCredential).toHaveBeenCalledWith({
        name: 'new-pat',
        kind: 'pat',
        secret: 'ghp_freshtoken',
        host: undefined,
      })
    );

    await waitFor(() => expect(screen.getByText('new-pat')).toBeInTheDocument());
  });

  it('renders the GitHub-rejection details inline on a 422', async () => {
    mocks.adminListCredentials.mockResolvedValue([]);
    mocks.adminCreateCredential.mockRejectedValue({
      code: 'VALIDATION_ERROR',
      error: 'credential rejected by GitHub',
      details: 'Bad credentials',
    });

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText(/no credentials/i)).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: /add credential/i }));

    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText(/^name$/i), { target: { value: 'bad-pat' } });
    fireEvent.change(within(dialog).getByLabelText(/secret/i), { target: { value: 'ghp_badtoken' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /add credential|save|create/i }));

    await waitFor(() => expect(mocks.adminCreateCredential).toHaveBeenCalled());
    await waitFor(() => expect(within(dialog).getByText(/bad credentials/i)).toBeInTheDocument());
    expect(within(dialog).getByText(/credential rejected by github/i)).toBeInTheDocument();
  });

  it('shows friendly kind-selector labels ("PAT" / "GitHub App"), not "GitHub PAT"', async () => {
    mocks.adminListCredentials.mockResolvedValue([]);

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText(/no credentials/i)).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /add credential/i }));

    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByRole('button', { name: 'PAT' })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'GitHub App' })).toBeInTheDocument();
  });

  it('falls back to a generic message and no details when create rejects with a non-APIError shape', async () => {
    mocks.adminListCredentials.mockResolvedValue([]);
    // Malformed rejection: neither field is a string, so both must be
    // rejected by the isAPIError guard rather than rendered verbatim.
    mocks.adminCreateCredential.mockRejectedValue({ error: 12345, details: 999 });

    render(<AdminCredentialsPage />);

    await waitFor(() => expect(screen.getByText(/no credentials/i)).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /add credential/i }));

    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText(/^name$/i), { target: { value: 'bad-pat' } });
    fireEvent.change(within(dialog).getByLabelText(/secret/i), { target: { value: 'ghp_badtoken' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /add credential|save|create/i }));

    await waitFor(() => expect(mocks.adminCreateCredential).toHaveBeenCalled());
    await waitFor(() => expect(within(dialog).getByText('Failed to save credential.')).toBeInTheDocument());
    expect(within(dialog).queryByText('12345')).not.toBeInTheDocument();
    expect(within(dialog).queryByText('999')).not.toBeInTheDocument();
  });
});
