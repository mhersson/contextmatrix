import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybookRow, PlaybookReceipt } from './PlaybookRow';
import type { PlaybookSummary } from '../../types';

function summary(over: Partial<PlaybookSummary> = {}): PlaybookSummary {
  return {
    id: 'rollout', title: 'Rollout', complete: 1, total: 3,
    segments: ['complete', 'pending', 'pending'], projects: 2, updated_at: '2020-01-01T00:00:00Z', ...over,
  };
}

describe('PlaybookRow', () => {
  it('links to the detail page and shows progress as a fraction', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary()} /></MemoryRouter>);
    expect(screen.getByRole('link', { name: /rollout/i }).getAttribute('href')).toBe('/playbooks/rollout');
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(screen.getByText(/of 3/)).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '1 of 3 complete' })).toBeInTheDocument();
  });

  it('writes the meta line as a sentence, not a dotted string', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary()} /></MemoryRouter>);
    expect(screen.getByText('3 entries across 2 projects')).toBeInTheDocument();
    expect(screen.queryByText(/·/)).not.toBeInTheDocument();
  });

  it('drops the project clause when only manual steps are listed', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ projects: 0 })} /></MemoryRouter>);
    expect(screen.getByText('3 entries')).toBeInTheDocument();
  });

  it('uses singular wording for one entry in one project', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ total: 1, complete: 0, segments: ['pending'], projects: 1 })} /></MemoryRouter>);
    expect(screen.getByText('1 entry across 1 project')).toBeInTheDocument();
  });

  it('falls back to an unknown-card label when the frontier card is missing', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ segments: ['complete', 'missing', 'pending'], next: { type: 'card', project: 'alpha', card: 'ALPHA-9', title: '' } })} /></MemoryRouter>);
    expect(screen.getByText('ALPHA-9')).toBeInTheDocument();
    expect(screen.getByText('(unknown card)')).toBeInTheDocument();
  });

  it('never labels a manual frontier as an unknown card', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ next: { type: 'manual', title: '' } })} /></MemoryRouter>);
    expect(screen.queryByText('(unknown card)')).not.toBeInTheDocument();
  });

  it('shows pills only when an agent is active or a card is missing', () => {
    const { rerender } = render(<MemoryRouter><PlaybookRow playbook={summary()} /></MemoryRouter>);
    expect(screen.queryByText('agent active')).not.toBeInTheDocument();
    expect(screen.queryByText('missing card')).not.toBeInTheDocument();

    rerender(<MemoryRouter><PlaybookRow playbook={summary({ segments: ['complete', 'active', 'missing'] })} /></MemoryRouter>);
    expect(screen.getByText('agent active')).toBeInTheDocument();
    expect(screen.getByText('missing card')).toBeInTheDocument();
  });

  it('names the frontier entry when the summary carries one', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ next: { type: 'card', project: 'alpha', card: 'ALPHA-7', title: 'Wire the flag' } })} /></MemoryRouter>);
    expect(screen.getByText('next up')).toBeInTheDocument();
    expect(screen.getByText('ALPHA-7')).toBeInTheDocument();
    expect(screen.getByText('Wire the flag')).toBeInTheDocument();
  });

  it('names a manual frontier by its text without a card id', () => {
    render(<MemoryRouter><PlaybookRow playbook={summary({ next: { type: 'manual', title: 'Confirm the push landed' } })} /></MemoryRouter>);
    expect(screen.getByText('next up')).toBeInTheDocument();
    expect(screen.getByText('Confirm the push landed')).toBeInTheDocument();
  });
});

describe('PlaybookReceipt', () => {
  it('renders a completed playbook as a one-line link with its entry count', () => {
    render(<MemoryRouter><PlaybookReceipt playbook={summary({ complete: 3, segments: ['complete', 'complete', 'complete'] })} /></MemoryRouter>);
    const link = screen.getByRole('link', { name: /rollout/i });
    expect(link.getAttribute('href')).toBe('/playbooks/rollout');
    expect(screen.getByText('3 entries')).toBeInTheDocument();
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });
});
