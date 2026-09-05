import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import type { CredentialInfo, ProjectConfig } from '../../types';
import { ProjectSettings } from './ProjectSettings';

// Mirrors GitHubCredentialSection.test.tsx's vi.hoisted + vi.mock convention
// for the api client module.
const mocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  getCards: vi.fn(),
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
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
      deleteProject: mocks.deleteProject,
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
  mocks.deleteProject.mockResolvedValue(undefined);
});

async function renderSettings(props: { onDeleted?: () => void } = {}) {
  const onDeleted = props.onDeleted ?? vi.fn();
  const view = render(
    <ProjectSettings project="alpha" onUpdated={vi.fn()} onDeleted={onDeleted} showToast={vi.fn()} />,
  );
  await waitFor(() => expect(mocks.getProject).toHaveBeenCalled());
  await screen.findByLabelText(/repository url/i);
  return { ...view, onDeleted };
}

function openTab(name: RegExp) {
  fireEvent.click(screen.getByRole('tab', { name }));
}

/** Accessible names of every tab currently carrying the unsaved dot. */
function dirtyTabs(): string[] {
  return screen.queryAllByRole('tab', { name: /unsaved changes/i }).map((t) => t.textContent ?? '');
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
    openTab(/execution/i);

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
    openTab(/execution/i);

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
    openTab(/execution/i);
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
    openTab(/execution/i);
    expect(screen.queryByLabelText(/agent worker image/i)).not.toBeInTheDocument();
  });

  it('chat picker is hidden when chat_enabled is false', async () => {
    mocks.useTheme.mockReturnValue({ chatEnabled: false, taskBackend: 'agent' });
    mocks.getProject.mockResolvedValue(baseConfig());

    await renderSettings();
    openTab(/execution/i);
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
    openTab(/execution/i);

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
    openTab(/execution/i);

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
    openTab(/execution/i);

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

describe('ProjectSettings - handleSave payload construction for card_defaults', () => {
  it('untouched: saving an unrelated field omits card_defaults from the PUT body', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ card_defaults: { autonomous: true } }));
    mocks.updateProject.mockResolvedValue(baseConfig({ card_defaults: { autonomous: true }, repo: 'x' }));

    await renderSettings();
    openTab(/automation/i);
    expect(screen.getByLabelText('Default autonomous mode')).toBeChecked();

    openTab(/source/i);
    fireEvent.change(screen.getByLabelText(/repository url/i), { target: { value: 'x' } });
    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body).not.toHaveProperty('card_defaults');
  });

  it('changed: enabling autonomous and mob seats sends the full explicit block', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.updateProject.mockResolvedValue(
      baseConfig({ card_defaults: { autonomous: true, mob_participants: 3, mob_phases: ['review'] } }),
    );

    await renderSettings();
    openTab(/automation/i);
    fireEvent.click(screen.getByLabelText('Default autonomous mode'));
    fireEvent.change(screen.getByLabelText('Default mob seats'), { target: { value: '3' } });

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body.card_defaults).toEqual({
      autonomous: true,
      max_capability: false,
      mob_participants: 3,
      mob_phases: ['review'],
      create_pr: true,
      await_ci: false,
      await_copilot_review: false,
    });
  });

  it('reset: turning a stored default back off sends the built-in block so the server clears it', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ card_defaults: { autonomous: true } }));
    mocks.updateProject.mockResolvedValue(baseConfig());

    await renderSettings();
    openTab(/automation/i);
    fireEvent.click(screen.getByLabelText('Default autonomous mode'));

    const saveButton = screen.getByRole('button', { name: /save/i });
    await waitFor(() => expect(saveButton).not.toBeDisabled());
    fireEvent.click(saveButton);

    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    const [, body] = mocks.updateProject.mock.calls[0];
    expect(body.card_defaults).toEqual({
      autonomous: false,
      max_capability: false,
      mob_participants: 0,
      create_pr: true,
      await_ci: false,
      await_copilot_review: false,
    });
  });

  it('Automation tab lists card defaults before task skills', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/automation/i);

    const defaults = screen.getByRole('heading', { name: /card defaults/i });
    const skills = screen.getByRole('heading', { name: /task skills/i });
    expect(defaults.compareDocumentPosition(skills) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe('ProjectSettings - rail tabs, header and dirty state', () => {
  it('renders five tabs with Source active and only its panel visible', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();

    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(5);
    ['Source', 'Workflow', 'Automation', 'Execution', 'Danger'].forEach((name, i) =>
      expect(tabs[i]).toHaveAccessibleName(name),
    );
    expect(screen.getByRole('tab', { name: 'Source' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tabpanel', { name: 'Source' })).toBeVisible();
    expect(screen.getByLabelText(/repository url/i)).toBeVisible();
    // Other panels stay mounted (state and fetches survive tab switches) but hidden.
    expect(screen.queryByRole('button', { name: 'Remove todo' })).not.toBeInTheDocument();
    expect(screen.getByLabelText(/verify command/i)).not.toBeVisible();
  });

  it('switching to Workflow shows the state chips and hides the repository field', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ states: ['todo', 'stalled', 'not_planned'] }));
    await renderSettings();
    openTab(/workflow/i);

    expect(screen.getByRole('tab', { name: 'Workflow' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tabpanel', { name: 'Workflow' })).toBeVisible();
    expect(screen.getByRole('tab', { name: 'Workflow' })).toHaveAttribute(
      'aria-controls',
      screen.getByRole('tabpanel').id,
    );
    expect(screen.getByRole('button', { name: 'Remove todo' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Remove stalled' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove not_planned' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'todo to stalled' })).toBeVisible();
    expect(screen.getByLabelText(/repository url/i)).not.toBeVisible();
  });

  it('marks only the edited tab as unsaved and clears the mark after saving', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.updateProject.mockResolvedValue(baseConfig({ repo: 'git@github.com:org/new.git' }));
    await renderSettings();

    expect(screen.queryByRole('tab', { name: /unsaved changes/i })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/repository url/i), {
      target: { value: 'git@github.com:org/new.git' },
    });

    expect(screen.getByRole('tab', { name: /source.*unsaved changes/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Workflow' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Execution' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.queryByRole('tab', { name: /unsaved changes/i })).not.toBeInTheDocument(),
    );
  });

  it('keeps the unsaved mark on a tab while another tab is open', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/execution/i);
    fireEvent.change(screen.getByLabelText(/verify command/i), { target: { value: 'make test' } });
    openTab(/workflow/i);

    expect(screen.getByRole('tab', { name: /execution.*unsaved changes/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Workflow' })).toHaveAttribute('aria-selected', 'true');
  });

  it('Discard restores the loaded config and disables Save', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();

    expect(screen.queryByRole('button', { name: /discard/i })).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/repository url/i), {
      target: { value: 'git@github.com:org/new.git' },
    });
    await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).not.toBeDisabled());

    fireEvent.click(screen.getByRole('button', { name: /discard/i }));

    expect(screen.getByLabelText(/repository url/i)).toHaveValue('git@github.com:org/alpha.git');
    expect(screen.getByRole('button', { name: /save/i })).toBeDisabled();
    expect(screen.queryByRole('button', { name: /discard/i })).not.toBeInTheDocument();
    expect(mocks.updateProject).not.toHaveBeenCalled();
  });

  it('header carries the display name, prefix chip and card count', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ boards_repo: 'alpha-boards' }));
    mocks.getCards.mockResolvedValue([{ id: 'ALPHA-1' }, { id: 'ALPHA-2' }]);
    await renderSettings();

    expect(screen.getByRole('heading', { name: 'Alpha' })).toBeInTheDocument();
    expect(screen.getByText('ALPHA')).toBeInTheDocument();
    expect(screen.getByText('2 cards')).toBeInTheDocument();
    // A single-repo instance has nothing to distinguish, so the repo stays out.
    expect(screen.queryByText('alpha-boards')).not.toBeInTheDocument();
  });

  it('header names the boards repo on a multi-repo instance', async () => {
    mocks.useTheme.mockReturnValue({
      chatEnabled: true,
      taskBackend: 'agent',
      boardsRepos: [{ name: 'alpha-boards' }, { name: 'team-boards' }],
    });
    mocks.getProject.mockResolvedValue(baseConfig({ boards_repo: 'alpha-boards' }));
    await renderSettings();

    expect(screen.getByText('alpha-boards')).toBeInTheDocument();
  });

  it('Danger tab explains why delete is blocked while cards exist', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.getCards.mockResolvedValue([{ id: 'ALPHA-1' }]);
    await renderSettings();
    openTab(/danger/i);

    expect(screen.getByRole('button', { name: /delete project/i })).toBeDisabled();
    expect(screen.getByText(/this project has 1 card\. delete every card first/i)).toBeInTheDocument();
  });

  it('read-only: swaps the buttons for the lock note, disables fields, tabs still switch', async () => {
    mocks.useOptionalAuth.mockReturnValue({
      mode: 'multi',
      user: { username: 'viewer', display_name: 'Viewer', is_admin: false },
    });
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();

    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument();
    expect(screen.getByText(/only admins/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/repository url/i)).toBeDisabled();

    openTab(/workflow/i);
    expect(screen.getByRole('button', { name: 'todo to in_progress' })).toBeDisabled();
  });

  it('"Constrain to selected skills" survives a tab switch before any skill is ticked', async () => {
    mocks.getTaskSkills.mockResolvedValue([{ name: 'go-development', description: 'Go' }]);
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/automation/i);
    fireEvent.click(screen.getByLabelText('Constrain to selected skills'));
    await screen.findByLabelText(/go-development/);
    openTab(/source/i);
    openTab(/automation/i);
    expect(screen.getByLabelText('Constrain to selected skills')).toBeChecked();
  });

  it('section fetches run once per page load, not once per tab visit', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/execution/i);
    openTab(/automation/i);
    openTab(/execution/i);
    openTab(/source/i);
    expect(mocks.getBackendImages).toHaveBeenCalledTimes(2);
    expect(mocks.getTaskSkills).toHaveBeenCalledTimes(1);
    expect(mocks.adminListCredentials).toHaveBeenCalledTimes(1);
  });

  it('switching project resets the active tab to Source and reloads', async () => {
    mocks.getProject.mockImplementation((p: string) =>
      Promise.resolve(baseConfig({ name: p, display_name: p, repo: `git@github.com:org/${p}.git` })),
    );
    const { rerender } = await renderSettings();
    openTab(/workflow/i);
    expect(screen.getByRole('tab', { name: 'Workflow' })).toHaveAttribute('aria-selected', 'true');
    rerender(<ProjectSettings project="beta" onUpdated={vi.fn()} onDeleted={vi.fn()} showToast={vi.fn()} />);
    await waitFor(() => expect(mocks.getProject).toHaveBeenCalledWith('beta'));
    expect(await screen.findByLabelText(/repository url/i)).toHaveValue('git@github.com:org/beta.git');
    expect(screen.getByRole('tab', { name: 'Source' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('heading', { name: 'beta' })).toBeInTheDocument();
  });

  it('Discard is disabled and Save reads Saving… while a save is in flight', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    let resolve!: (v: ProjectConfig) => void;
    mocks.updateProject.mockReturnValue(new Promise<ProjectConfig>((r) => { resolve = r; }));
    await renderSettings();
    fireEvent.change(screen.getByLabelText(/repository url/i), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    expect(await screen.findByRole('button', { name: 'Saving…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: /discard/i })).toBeDisabled();
    await act(async () => { resolve(baseConfig({ repo: 'x' })); });
    expect(screen.queryByRole('button', { name: /discard/i })).not.toBeInTheDocument();
  });
});

describe('ProjectSettings - unsaved dots per tab', () => {
  beforeEach(() => {
    mocks.getProject.mockResolvedValue(baseConfig());
  });

  it('Workflow dots when a type is added', async () => {
    await renderSettings();
    openTab(/workflow/i);
    fireEvent.change(screen.getByLabelText('Types'), { target: { value: 'bug' } });
    fireEvent.click(screen.getAllByRole('button', { name: 'Add' })[1]);
    expect(dirtyTabs()).toEqual(['Workflow (unsaved changes)']);
  });

  it('Workflow dots when a transition is toggled', async () => {
    await renderSettings();
    openTab(/workflow/i);
    fireEvent.click(screen.getByRole('button', { name: 'done to todo' }));
    expect(dirtyTabs()).toEqual(['Workflow (unsaved changes)']);
  });

  it('Automation dots when a card default is toggled', async () => {
    await renderSettings();
    openTab(/automation/i);
    fireEvent.click(screen.getByLabelText('Default autonomous mode'));
    expect(dirtyTabs()).toEqual(['Automation (unsaved changes)']);
  });

  it('Automation dots when default skills change', async () => {
    await renderSettings();
    openTab(/automation/i);
    fireEvent.click(screen.getByLabelText('Mount no skills'));
    expect(dirtyTabs()).toEqual(['Automation (unsaved changes)']);
  });

  it('Source dots when issue import is switched on, and the save carries the fields', async () => {
    mocks.updateProject.mockResolvedValue(
      baseConfig({ github: { import_issues: true, labels: ['bug', 'help wanted'] } }),
    );
    await renderSettings();
    expect(screen.queryByLabelText(/filter by github labels/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/import open issues from github/i));
    fireEvent.change(screen.getByLabelText(/filter by github labels/i), {
      target: { value: 'bug, help wanted' },
    });
    expect(dirtyTabs()).toEqual(['Source (unsaved changes)']);
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    expect(mocks.updateProject.mock.calls[0][1].github).toEqual({
      import_issues: true,
      labels: ['bug', 'help wanted'],
    });
  });
});

describe('ProjectSettings - Discard across tabs', () => {
  it('Discard on a non-active tab resets that section and clears its dot', async () => {
    mocks.getProject.mockResolvedValue(baseConfig({ verify: { command: 'make test' } }));
    await renderSettings();
    openTab(/execution/i);
    fireEvent.change(screen.getByLabelText(/verify command/i), { target: { value: 'go test ./...' } });
    openTab(/source/i);
    expect(dirtyTabs()).toEqual(['Execution (unsaved changes)']);
    fireEvent.click(screen.getByRole('button', { name: /discard/i }));
    expect(dirtyTabs()).toHaveLength(0);
    openTab(/execution/i);
    expect(screen.getByLabelText(/verify command/i)).toHaveValue('make test');
  });

  it('Discard clears the remote_execution touched flag so a later save omits the key', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    mocks.getBackendImages.mockResolvedValue({ ok: true, images: [{ tags: ['ghcr.io/org/worker:latest'] }] });
    mocks.updateProject.mockResolvedValue(baseConfig({ repo: 'x' }));
    await renderSettings();
    openTab(/execution/i);
    const sel = await screen.findByLabelText(/agent worker image/i);
    await screen.findAllByRole('option', { name: 'ghcr.io/org/worker:latest' });
    fireEvent.change(sel, { target: { value: 'ghcr.io/org/worker:latest' } });
    expect(dirtyTabs()).toEqual(['Execution (unsaved changes)']);
    fireEvent.click(screen.getByRole('button', { name: /discard/i }));
    expect(dirtyTabs()).toHaveLength(0);
    expect(screen.getByRole('button', { name: /save/i })).toBeDisabled();
    expect(screen.getByLabelText(/agent worker image/i)).toHaveValue('');
    openTab(/source/i);
    fireEvent.change(screen.getByLabelText(/repository url/i), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    expect(mocks.updateProject.mock.calls[0][1]).not.toHaveProperty('remote_execution');
  });

  it('visiting Execution without touching it keeps remote_execution out of the PUT body', async () => {
    mocks.getProject.mockResolvedValue(
      baseConfig({ remote_execution: { worker_image: 'ghcr.io/org/worker:latest' } }),
    );
    mocks.getBackendImages.mockResolvedValue({ ok: true, images: [{ tags: ['ghcr.io/org/worker:latest'] }] });
    mocks.updateProject.mockResolvedValue(baseConfig({ repo: 'x' }));
    await renderSettings();
    openTab(/execution/i);
    await screen.findAllByRole('option', { name: 'ghcr.io/org/worker:latest' });
    expect(dirtyTabs()).toHaveLength(0);
    openTab(/source/i);
    openTab(/execution/i);
    expect(dirtyTabs()).toHaveLength(0);
    openTab(/source/i);
    fireEvent.change(screen.getByLabelText(/repository url/i), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(mocks.updateProject).toHaveBeenCalled());
    expect(mocks.updateProject.mock.calls[0][1]).not.toHaveProperty('remote_execution');
  });
});

describe('ProjectSettings - Danger tab delete flow', () => {
  it('zero cards enables delete; confirming calls deleteProject and onDeleted', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    const { onDeleted } = await renderSettings();
    openTab(/danger/i);
    const del = screen.getByRole('button', { name: 'Delete project' });
    expect(del).toBeEnabled();
    fireEvent.click(del);
    expect(mocks.deleteProject).not.toHaveBeenCalled();
    const dialog = screen.getByRole('dialog', { name: /delete project alpha\?/i });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    });
    expect(dialog).not.toBeInTheDocument();
    expect(mocks.deleteProject).toHaveBeenCalledWith('alpha');
    expect(onDeleted).toHaveBeenCalledOnce();
  });

  it('Cancel closes the dialog without deleting', async () => {
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/danger/i);
    fireEvent.click(screen.getByRole('button', { name: 'Delete project' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(mocks.deleteProject).not.toHaveBeenCalled();
  });

  it('read-only viewer cannot open the confirm even with zero cards', async () => {
    mocks.useOptionalAuth.mockReturnValue({
      mode: 'multi',
      user: { username: 'viewer', display_name: 'Viewer', is_admin: false },
    });
    mocks.getProject.mockResolvedValue(baseConfig());
    await renderSettings();
    openTab(/danger/i);
    expect(screen.getByRole('button', { name: 'Delete project' })).toBeDisabled();
  });
});
