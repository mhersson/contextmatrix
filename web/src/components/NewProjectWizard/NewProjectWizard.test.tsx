import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NewProjectWizard } from './NewProjectWizard';
import { api } from '../../api/client';
import type { BoardsRepoInfo } from '../../types';

const themeState = vi.hoisted(() => ({ boardsRepos: [] as BoardsRepoInfo[] }));

vi.mock('../../api/client', () => ({
  api: { createProject: vi.fn() },
  isAPIError: () => false,
}));

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ boardsRepos: themeState.boardsRepos }),
}));

function fillAndCreate() {
  fireEvent.change(screen.getByPlaceholderText('My Project'), { target: { value: 'My Project' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create' }));
}

describe('NewProjectWizard - boards repo', () => {
  beforeEach(() => {
    vi.mocked(api.createProject).mockResolvedValue({ name: 'my-project', prefix: 'MYPROJECT', next_id: 1, states: [], types: [], priorities: [], transitions: {} });
  });

  afterEach(() => {
    themeState.boardsRepos = [];
    vi.mocked(api.createProject).mockReset();
  });

  it('shows no repo field and sends no boards_repo with one repo', async () => {
    themeState.boardsRepos = [{ name: 'boards', shared: false }];
    render(<NewProjectWizard onClose={() => {}} onCreated={() => {}} />);
    expect(screen.queryByRole('radiogroup', { name: 'Boards repo' })).toBeNull();
    fillAndCreate();
    await waitFor(() => expect(vi.mocked(api.createProject)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.createProject).mock.calls[0][0]).not.toHaveProperty('boards_repo');
  });

  it('offers the repos first, defaults to the first and sends the choice', async () => {
    themeState.boardsRepos = [{ name: 'team', shared: true }, { name: 'private', shared: false }];
    render(<NewProjectWizard onClose={() => {}} onCreated={() => {}} />);
    const group = screen.getByRole('radiogroup', { name: 'Boards repo' });
    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(2);
    expect(radios[0]).toBeChecked();
    expect(group.compareDocumentPosition(screen.getByPlaceholderText('My Project')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    fireEvent.click(screen.getByLabelText(/private/));
    fillAndCreate();
    await waitFor(() => expect(vi.mocked(api.createProject)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.createProject).mock.calls[0][0]).toMatchObject({ boards_repo: 'private' });
  });
});
