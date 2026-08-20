import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybookEntryRowView } from './PlaybookEntryRow';
import type { PlaybookEntry } from '../../types';

const noop = { onToggleDone: vi.fn(), onSaveNote: vi.fn(), onSaveText: vi.fn(), onRemove: vi.fn() };

function cardEntry(over: Partial<PlaybookEntry> = {}): PlaybookEntry {
  return {
    id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-101',
    card_title: 'Do the thing', card_state: 'in_progress', complete: false, ...over,
  };
}

describe('PlaybookEntryRowView', () => {
  it('renders a card entry with project badge, mono id, and state chip', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry()} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('ALPHA-101')).toBeInTheDocument();
    expect(screen.getByText('Do the thing')).toBeInTheDocument();
    expect(screen.getByText(/agent active/i)).toBeInTheDocument();
  });

  it('renders a missing card ref with a warning chip and keeps the entry', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry({ missing: true, card_title: undefined, card_state: undefined })} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.getByText(/missing/i)).toBeInTheDocument();
    expect(screen.getByText('ALPHA-101')).toBeInTheDocument();
  });

  it('toggles a manual entry through its checkbox', () => {
    const onToggleDone = vi.fn();
    render(<MemoryRouter><PlaybookEntryRowView
      entry={{ id: 'e2', type: 'manual', text: 'redeploy', complete: false }}
      index={1} isFrontier={false} {...noop} onToggleDone={onToggleDone} /></MemoryRouter>);
    fireEvent.click(screen.getByRole('checkbox', { name: /redeploy/i }));
    expect(onToggleDone).toHaveBeenCalledWith('e2', true);
  });

  it('shows the note in the display serif voice', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry({ note: 'brew coffee first' })} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    const note = screen.getByText(/brew coffee first/);
    expect(note).toHaveStyle({ fontStyle: 'italic' });
  });

  it('marks the frontier row', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry()} index={0} isFrontier={true} {...noop} /></MemoryRouter>);
    expect(screen.getByLabelText('next up')).toBeInTheDocument();
  });

  it('formats done_at as a relative time, not a raw ISO string', () => {
    const doneAt = '2020-01-01T00:00:00Z';
    render(<MemoryRouter><PlaybookEntryRowView
      entry={{ id: 'e3', type: 'manual', text: 'redeploy', complete: true, done: true, done_by: 'human:alice', done_at: doneAt }}
      index={1} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.queryByText(doneAt)).not.toBeInTheDocument();
    expect(screen.getByText(/\d+d ago/)).toBeInTheDocument();
  });
});
