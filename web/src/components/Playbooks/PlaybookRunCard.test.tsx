import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PlaybookRunCard } from './PlaybookRunCard';
import type { PlaybookDetail } from '../../types';

function detail(overrides: Partial<PlaybookDetail> = {}): PlaybookDetail {
  return {
    id: 'roll', title: 'Roll', created_at: 'x', updated_at: 'x', complete: 0, total: 2,
    entries: [
      { id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-1', card_title: 'First', card_state: 'todo', complete: false },
      { id: 'e2', type: 'card', project: 'beta', card: 'BETA-1', card_title: 'Second', card_state: 'todo', complete: false },
    ],
    ...overrides,
  };
}

function renderCard(d: PlaybookDetail, branches: string[] = ['develop', 'main']) {
  const handlers = {
    onToggleRunnable: vi.fn(), onSaveBaseBranch: vi.fn(), onRun: vi.fn(), onStop: vi.fn(),
  };
  render(<PlaybookRunCard detail={d} branches={branches} branchesLoading={false} branchesError={false} {...handlers} />);
  return handlers;
}

describe('PlaybookRunCard', () => {
  it('offers the switch and a base-branch dropdown when not runnable', () => {
    const h = renderCard(detail());
    const toggle = screen.getByRole('checkbox', { name: /make runnable/i });
    expect(toggle).not.toBeChecked();
    fireEvent.click(toggle);
    expect(h.onToggleRunnable).toHaveBeenCalledWith(true);

    const select = screen.getByRole('combobox', { name: /base branch/i });
    expect(select).toHaveValue('');
    expect(screen.getByRole('option', { name: 'Default branch' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'develop' })).toBeInTheDocument();
    fireEvent.change(select, { target: { value: 'main' } });
    expect(h.onSaveBaseBranch).toHaveBeenCalledWith('main');
    expect(screen.queryByRole('button', { name: /^run$/i })).not.toBeInTheDocument();
  });

  it('keeps a base branch that is not in the shared list selectable', () => {
    renderCard(detail({ base_branch: 'release/1.0' }), ['main']);
    const select = screen.getByRole('combobox', { name: /base branch/i });
    expect(select).toHaveValue('release/1.0');
    expect(screen.getByRole('option', { name: 'release/1.0' })).toBeInTheDocument();
  });

  it('shows the branch and a Run button when runnable and idle', () => {
    const h = renderCard(detail({ runnable: true, branch: 'playbook/roll', base_branch: 'main' }));
    expect(screen.getByText('playbook/roll')).toBeInTheDocument();
    expect(screen.getByText('Not started')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /^run$/i }));
    expect(h.onRun).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('checkbox', { name: /make runnable/i })).toBeChecked();
  });

  it('shows the status line and Stop while active, and disables the switch and input', () => {
    const h = renderCard(detail({
      runnable: true, branch: 'playbook/roll',
      run: { status: 'waiting', started_at: 'x', updated_at: 'x', entry: 'e2', reason: 'BETA-1 parked: merge refused' },
    }));
    expect(screen.getByText('Waiting for you: BETA-1 parked: merge refused')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /make runnable/i })).toBeDisabled();
    expect(screen.getByRole('combobox', { name: /base branch/i })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: /^stop$/i }));
    expect(h.onStop).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: /^run$/i })).not.toBeInTheDocument();
  });

  it('offers Run again after a stop', () => {
    renderCard(detail({
      runnable: true, branch: 'playbook/roll',
      run: { status: 'stopped', started_at: 'x', updated_at: 'x', reason: 'stopped by human:alice' },
    }));
    expect(screen.getByText('Stopped')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^run$/i })).toBeInTheDocument();
  });

  it('shows one compare link per repository when completed', () => {
    renderCard(detail({
      runnable: true, branch: 'playbook/roll',
      run: { status: 'completed', started_at: 'x', updated_at: 'x' },
      repos: [
        { project: 'alpha', compare_url: 'https://github.com/acme/alpha/compare/main...playbook/roll?expand=1' },
        { project: 'beta', compare_url: 'https://github.com/acme/beta/compare/playbook/roll?expand=1' },
      ],
    }));
    expect(screen.getByText('Completed')).toBeInTheDocument();
    const links = screen.getAllByRole('link', { name: /open pr/i });
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveAttribute('href', 'https://github.com/acme/alpha/compare/main...playbook/roll?expand=1');
    expect(links[0]).toHaveAttribute('target', '_blank');
  });
});
