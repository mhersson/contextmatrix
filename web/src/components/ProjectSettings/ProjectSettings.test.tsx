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
  getBackendImages: vi.fn(),
  useOptionalAuth: vi.fn(),
  useTheme: vi.fn(),
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
      // calls this on mount - not otherwise relevant to this test file.
      getTaskSkills: mocks.getTaskSkills,
      getBackendImages: mocks.getBackendImages,
    },
  };
});

// Mocked directly (rather than mounting a real AuthProvider) so mode/isAdmin
// are asserted without an extra getAppConfig/getAuthSession round-trip -
// mirrors web/src/hooks/useIdentity.test.tsx's vi.mock('./useAuth', ...) style.
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: mocks.useOptionalAuth,
}));

// ProjectSettings now consumes useTheme() for chatEnabled; the test file
// renders without a ThemeProvider, so mock the hook directly.
vi.mock('../../hooks/useTheme', () => ({
  useTheme: mocks.useTheme,
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
  mocks.getBackendImages.mockResolvedValue({ ok: true, images: [] });
  mocks.useTheme.mockReturnValue({ chatEnabled: true, taskBackend: 'agent' });
});

async function renderSettings() {
  render(
    <ProjectSettings project="alpha" onUpdated={vi.fn()} onDeleted={vi.fn()} showToast={vi.fn()} />,
  );
  await waitFor(() => expect(mocks.getProject).toHaveBeenCalled());
  await screen.findByLabelText(/repository url/i);
}

describe('ProjectSettings - handleSave payload construction for github_credential', () => {
  it('untouched stale binding: saving an unrelated field omits github_credential from the PUT body', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ github_credential: 'ghost' }));
    mocks.adminListCredentials.mockResolvedValue([credential({ name: 'acme-pat' })]);
    mocks.updateProject.mockResolvedValue(
      baseConfig({ github_credential: 'ghost', repo: 'git@github.com:org/new.git' }),
    );

    await renderSettings();

    // "ghost" is not in the pool - GitHubCredentialSection shows the stale warning.
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

describe('ProjectSettings - handleSave payload construction for remote_execution', () => {
  it('untouched: saving an unrelated field omits remote_execution from the PUT body', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.updateProject.mockResolvedValue(baseConfig({ repo: 'git@github.com:org/new.git' }));

    await renderSettings();

    // Edit an unrelated field (repo URL) without touching remote execution.
    fireEvent.change(screen.getByLabelText(/repository url/i), {
      target: { value: 'git@github.com:org/new.git' },
    });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body).not.toHaveProperty('remote_execution');
  });

  it('changed: picking a task image sends the images-only payload', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.getBackendImages.mockResolvedValue({
      ok: true,
      images: [{ tags: ['ghcr.io/org/worker:latest'] }],
    });
    mocks.updateProject.mockResolvedValue(
      baseConfig({ remote_execution: { worker_image: 'ghcr.io/org/worker:latest' } }),
    );

    await renderSettings();

    const imageSelect = await screen.findByLabelText(/agent worker image/i);
    fireEvent.change(imageSelect, { target: { value: 'ghcr.io/org/worker:latest' } });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body.remote_execution).toEqual({
      worker_image: 'ghcr.io/org/worker:latest',
      chat_worker_image: '',
    });
  });

  it('changed: picking a chat image sends chat_worker_image in the payload', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.getBackendImages.mockImplementation((backend: string) =>
      Promise.resolve({
        ok: true,
        images:
          backend === 'chat'
            ? [{ tags: ['contextmatrix-chat-worker:go-node'] }]
            : [{ tags: ['contextmatrix-agent-worker:go-node'] }],
      }),
    );
    mocks.updateProject.mockResolvedValue(
      baseConfig({ remote_execution: { chat_worker_image: 'contextmatrix-chat-worker:go-node' } }),
    );

    await renderSettings();

    const chatSelect = await screen.findByLabelText(/chat worker image/i);
    await screen.findByRole('option', { name: 'contextmatrix-chat-worker:go-node' });
    fireEvent.change(chatSelect, { target: { value: 'contextmatrix-chat-worker:go-node' } });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    // The payload is always the two image fields - nothing else exists to send.
    expect(body.remote_execution).toEqual({
      worker_image: '',
      chat_worker_image: 'contextmatrix-chat-worker:go-node',
    });
  });

  it('task picker is gated on a configured task backend; no enable toggle exists', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());

    await renderSettings();
    expect(screen.getByLabelText(/agent worker image/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/chat worker image/i)).toBeInTheDocument();
    expect(
      screen.queryByRole('checkbox', { name: /enable remote execution/i }),
    ).not.toBeInTheDocument();
  });

  it('task picker is hidden when no task backend is configured', async () => {
    mocks.useTheme.mockReturnValue({ chatEnabled: true, taskBackend: '' });
    mocks.getProject.mockResolvedValue(baseConfig());

    await renderSettings();
    expect(screen.queryByLabelText(/agent worker image/i)).not.toBeInTheDocument();
  });

  it('chat picker is hidden when chat_enabled is false', async () => {
    mocks.useTheme.mockReturnValue({ chatEnabled: false, taskBackend: 'agent' });
    mocks.getProject.mockResolvedValue(baseConfig());

    await renderSettings();
    expect(screen.queryByLabelText(/chat worker image/i)).not.toBeInTheDocument();
  });

  it('consecutive unrelated-field saves keep omitting remote_execution', async () => {
    mocks.getProject.mockResolvedValue(
      baseConfig({ remote_execution: { worker_image: 'ghcr.io/org/worker:latest' } }),
    );
    mocks.updateProject.mockImplementation((_project: string, input: { repo?: string }) =>
      Promise.resolve(
        baseConfig({
          repo: input.repo ?? '',
          remote_execution: { worker_image: 'ghcr.io/org/worker:latest' },
        }),
      ),
    );

    await renderSettings();

    const repoInput = screen.getByLabelText(/repository url/i);

    fireEvent.change(repoInput, { target: { value: 'git@github.com:org/two.git' } });
    await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalledTimes(1));
    expect(mocks.updateProject.mock.calls[0][1]).not.toHaveProperty('remote_execution');

    // The PUT response becoming the baseline must not leave the form dirty.
    await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).toBeDisabled());

    fireEvent.change(repoInput, { target: { value: 'git@github.com:org/three.git' } });
    await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalledTimes(2));
    expect(mocks.updateProject.mock.calls[1][1]).not.toHaveProperty('remote_execution');
  });
});

describe('ProjectSettings - handleSave payload construction for verify', () => {
  it('untouched: saving an unrelated field omits verify from the PUT body', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ verify: { command: 'make test' } }));
    mocks.updateProject.mockResolvedValue(baseConfig({ repo: 'git@github.com:org/new.git' }));

    await renderSettings();

    fireEvent.change(screen.getByLabelText(/repository url/i), {
      target: { value: 'git@github.com:org/new.git' },
    });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body).not.toHaveProperty('verify');
  });

  it('changed: setting a command, timeout, and env sends the full verify object', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.updateProject.mockResolvedValue(
      baseConfig({ verify: { command: 'make test', timeout_seconds: 300, env: ['JAVA_HOME'] } }),
    );

    await renderSettings();

    fireEvent.change(screen.getByLabelText(/verify command/i), {
      target: { value: 'make test' },
    });
    fireEvent.change(screen.getByLabelText(/timeout \(seconds\)/i), {
      target: { value: '300' },
    });
    fireEvent.change(screen.getByLabelText(/passthrough env names/i), {
      target: { value: 'JAVA_HOME, CGO_ENABLED' },
    });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body.verify).toEqual({
      command: 'make test',
      timeout_seconds: 300,
      env: ['JAVA_HOME', 'CGO_ENABLED'],
    });
  });

  it('cleared: emptying every field sends a zero-value verify object (server clears it)', async () => {
    mocks.getProject.mockResolvedValue(
      baseConfig({ verify: { command: 'make test', timeout_seconds: 600 } }),
    );
    mocks.updateProject.mockResolvedValue(baseConfig());

    await renderSettings();

    fireEvent.change(screen.getByLabelText(/verify command/i), { target: { value: '' } });
    fireEvent.change(screen.getByLabelText(/timeout \(seconds\)/i), { target: { value: '' } });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    // A zero-value verify object clears it on the server; env is omitted (empty
    // env carries no intent at the project level).
    expect(body.verify).toEqual({ command: '', timeout_seconds: 0 });
    expect(body.verify).not.toHaveProperty('env');
  });

  it('command only: omits env from the verify object so .board.yaml stays clean', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.updateProject.mockResolvedValue(baseConfig({ verify: { command: 'make test' } }));

    await renderSettings();

    fireEvent.change(screen.getByLabelText(/verify command/i), {
      target: { value: 'make test' },
    });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body.verify).toEqual({ command: 'make test', timeout_seconds: 0 });
    expect(body.verify).not.toHaveProperty('env');
  });
});
