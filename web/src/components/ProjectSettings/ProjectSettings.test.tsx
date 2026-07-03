import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import type { CredentialInfo, ProjectConfig } from '../../types';
import { ProjectSettings } from './ProjectSettings';

// Mirrors GitHubCredentialSection.test.tsx's vi.hoisted + vi.mock convention
// for the api client module.
const mocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  getCards: vi.fn(),
  updateProject: vi.fn(),
  adminListCredentials: vi.fn(),
  getTaskSkills: vi.fn(),
  useOptionalAuth: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      getProject: mocks.getProject,
      getCards: mocks.getCards,
      updateProject: mocks.updateProject,
      adminListCredentials: mocks.adminListCredentials,
      // DefaultSkillsSelector (always mounted as a ProjectSettings child)
      // calls this on mount — not otherwise relevant to this test file.
      getTaskSkills: mocks.getTaskSkills,
    },
  };
});

// Mocked directly (rather than mounting a real AuthProvider) so mode/isAdmin
// are asserted without an extra getAppConfig/getAuthSession round-trip —
// mirrors web/src/hooks/useIdentity.test.tsx's vi.mock('./useAuth', ...) style.
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: mocks.useOptionalAuth,
}));

function baseConfig(overrides: Partial<ProjectConfig> = {}): ProjectConfig {
  return {
    name: 'alpha',
    display_name: 'Alpha',
    prefix: 'ALPHA',
    next_id: 1,
    repo: 'git@github.com:org/alpha.git',
    states: ['todo', 'in_progress', 'done'],
    types: ['task'],
    priorities: ['medium'],
    transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
    ...overrides,
  };
}

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
  mocks.useOptionalAuth.mockReturnValue({
    mode: 'multi',
    user: { username: 'admin', display_name: 'Admin', is_admin: true },
  });
  mocks.getCards.mockResolvedValue([]);
  mocks.adminListCredentials.mockResolvedValue([]);
  mocks.getTaskSkills.mockResolvedValue([]);
});

async function renderSettings() {
  render(
    <ProjectSettings project="alpha" onUpdated={vi.fn()} onDeleted={vi.fn()} showToast={vi.fn()} />,
  );
  await waitFor(() => expect(mocks.getProject).toHaveBeenCalled());
  await screen.findByLabelText(/repository url/i);
}

describe('ProjectSettings — handleSave payload construction for github_credential', () => {
  it('untouched stale binding: saving an unrelated field omits github_credential from the PUT body', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ github_credential: 'ghost' }));
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);
    mocks.updateProject.mockResolvedValue(
      baseConfig({ github_credential: 'ghost', repo: 'git@github.com:org/new.git' }),
    );

    await renderSettings();

    // "ghost" is not in the pool — GitHubCredentialSection shows the stale warning.
    await screen.findByText(/credential no longer exists/i);

    // Edit an unrelated field (repo URL) without touching the credential select.
    fireEvent.change(screen.getByLabelText(/repository url/i), {
      target: { value: 'git@github.com:org/new.git' },
    });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body).not.toHaveProperty('github_credential');
  });

  it('changed binding to instance default: PUT body carries github_credential: ""', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ github_credential: 'acme-pat' }));
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);
    mocks.updateProject.mockResolvedValue(baseConfig({ github_credential: '' }));

    await renderSettings();

    const select = await screen.findByRole('combobox', { name: /github credential/i });
    await waitFor(() => expect(select).toHaveValue('acme-pat'));

    fireEvent.change(select, { target: { value: '' } });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body).toHaveProperty('github_credential', '');
  });
});
