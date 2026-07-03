import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import type { CredentialInfo } from '../../types';
import { GitHubCredentialSection } from './GitHubCredentialSection';

const mocks = vi.hoisted(() => ({
  adminListCredentials: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminListCredentials: mocks.adminListCredentials,
    },
  };
});

function credential(overrides: Partial<CredentialInfo> = {}): CredentialInfo {
  return {
    name: 'acme-pat',
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

beforeEach(() => {
  vi.resetAllMocks();
});

describe('GitHubCredentialSection — admin (select) mode', () => {
  it('renders "Instance default" plus one option per pool entry, disabled entries suffixed', async () => {
    mocks.adminListCredentials.mockResolvedValue([
      credential({ name: 'acme-pat', kind: 'pat', host: '' }),
      credential({ name: 'acme-app', kind: 'app', host: 'github.example.com', disabled: true }),
    ]);

    render(<GitHubCredentialSection value="" onChange={vi.fn()} readOnly={false} />);

    await waitFor(() => expect(mocks.adminListCredentials).toHaveBeenCalled());

    expect(await screen.findByRole('option', { name: /instance default/i })).toBeInTheDocument();
    expect(
      await screen.findByRole('option', { name: /acme-pat — pat, github\.com/i }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole('option', {
        name: /acme-app — app, github\.example\.com \(disabled\)/i,
      }),
    ).toBeInTheDocument();
  });

  it('shows a red warning when the bound credential is missing from the pool, and preserves the stale option', async () => {
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);

    render(<GitHubCredentialSection value="deleted-cred" onChange={vi.fn()} readOnly={false} />);

    await waitFor(() => expect(mocks.adminListCredentials).toHaveBeenCalled());

    const warning = await screen.findByText(/credential no longer exists — operations on this project will fail/i);
    expect(warning).toBeInTheDocument();
    expect(warning).toHaveStyle({ color: 'var(--red)' });

    const select = screen.getByRole('combobox') as HTMLSelectElement;
    expect(select.value).toBe('deleted-cred');
    expect(screen.getByRole('option', { name: /deleted-cred/i })).toBeInTheDocument();
  });

  it('does not show a warning while the bound credential is present in the pool', async () => {
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);

    render(<GitHubCredentialSection value="acme-pat" onChange={vi.fn()} readOnly={false} />);

    await waitFor(() => expect(screen.getByRole('combobox')).toHaveValue('acme-pat'));

    expect(screen.queryByText(/credential no longer exists/i)).not.toBeInTheDocument();
  });

  it('calls onChange with the newly selected credential name', async () => {
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);
    const onChange = vi.fn();

    render(<GitHubCredentialSection value="" onChange={onChange} readOnly={false} />);

    await screen.findByRole('option', { name: /acme-pat — pat, github\.com/i });

    const select = screen.getByRole('combobox') as HTMLSelectElement;
    select.value = 'acme-pat';
    select.dispatchEvent(new Event('change', { bubbles: true }));

    expect(onChange).toHaveBeenCalledWith('acme-pat');
  });
});

describe('GitHubCredentialSection — readOnly mode', () => {
  it('renders the bound name as plain text, not a select, and does not fetch the pool', () => {
    render(<GitHubCredentialSection value="acme-pat" onChange={vi.fn()} readOnly />);

    expect(screen.getByText('acme-pat')).toBeInTheDocument();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(mocks.adminListCredentials).not.toHaveBeenCalled();
  });

  it('renders "Instance default" as plain text when unbound', () => {
    render(<GitHubCredentialSection value="" onChange={vi.fn()} readOnly />);

    expect(screen.getByText(/instance default/i)).toBeInTheDocument();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  });
});
