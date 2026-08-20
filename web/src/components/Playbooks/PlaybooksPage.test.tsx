import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybooksPage } from './PlaybooksPage';
import { api } from '../../api/client';
import type { PlaybookDetail } from '../../types';

vi.mock('../../api/client', () => ({
  api: {
    listPlaybooks: vi.fn().mockResolvedValue([
      { id: 'active-one', title: 'Active one', complete: 1, total: 3, segments: ['complete', 'active', 'pending'], projects: 2, updated_at: '2026-08-20T09:00:00Z' },
      { id: 'done-one', title: 'Done one', complete: 2, total: 2, segments: ['complete', 'complete'], projects: 1, updated_at: '2026-08-19T09:00:00Z' },
    ]),
    createPlaybook: vi.fn(),
  },
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, error: null, reconnectEpoch: 0 }),
}));

vi.mock('../../hooks/useToast', () => ({
  useToast: () => ({ showToast: () => {} }),
}));

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: false, toggle: () => {}, close: () => {} }),
}));

describe('PlaybooksPage', () => {
  it('folds fully-complete playbooks behind the Completed toggle', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    expect(await screen.findByText('Active one')).toBeInTheDocument();
    expect(screen.queryByText('Done one')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /completed \(1\)/i }));
    expect(screen.getByText('Done one')).toBeInTheDocument();
  });

  it('links rows to the playbook detail page', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    const row = await screen.findByRole('link', { name: /active one/i });
    expect(row.getAttribute('href')).toBe('/playbooks/active-one');
  });

  it('disables Create while the request is in flight and labels the title input', async () => {
    let resolveCreate: (value: PlaybookDetail) => void = () => {};
    vi.mocked(api.createPlaybook).mockReturnValue(new Promise((resolve) => { resolveCreate = resolve; }));
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    const titleInput = screen.getByLabelText('Playbook title');
    fireEvent.change(titleInput, { target: { value: 'New rollout' } });
    const createButton = screen.getByRole('button', { name: /^create$/i });
    fireEvent.click(createButton);
    expect(createButton).toBeDisabled();

    fireEvent.click(createButton);
    expect(api.createPlaybook).toHaveBeenCalledTimes(1);

    resolveCreate({ id: 'new-rollout', title: 'New rollout', created_at: '', updated_at: '', complete: 0, total: 0, entries: [] });
    await waitFor(() => expect(createButton).not.toBeDisabled());
  });
});
