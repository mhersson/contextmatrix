import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybooksPage } from './PlaybooksPage';

vi.mock('../../api/client', () => ({
  api: {
    listPlaybooks: vi.fn().mockResolvedValue([
      { id: 'active-one', title: 'Active one', complete: 1, total: 3, segments: ['complete', 'active', 'pending'], projects: 2, updated_at: '2026-08-20T09:00:00Z' },
      { id: 'done-one', title: 'Done one', complete: 2, total: 2, segments: ['complete', 'complete'], projects: 1, updated_at: '2026-08-19T09:00:00Z' },
    ]),
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
});
