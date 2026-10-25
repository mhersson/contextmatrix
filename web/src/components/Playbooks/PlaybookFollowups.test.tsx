import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PlaybookFollowups } from './PlaybookFollowups';
import type { PlaybookFollowup } from '../../types';

function followup(over: Partial<PlaybookFollowup> = {}): PlaybookFollowup {
  return { id: 'f1', text: 'Check the dashboard', cards: [], done: false, ...over };
}

function setup(followups: PlaybookFollowup[], locked: boolean) {
  const handlers = {
    onToggle: vi.fn(),
    onSaveText: vi.fn(),
    onRemove: vi.fn(),
    onAdd: vi.fn(),
    onMove: vi.fn(),
  };
  render(<PlaybookFollowups followups={followups} locked={locked} {...handlers} />);
  return handlers;
}

describe('PlaybookFollowups', () => {
  it('disables ticks but keeps editing while the run is active', () => {
    const h = setup([followup()], true);
    expect(screen.getByRole('checkbox', { name: 'Check the dashboard' })).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: 'Check the dashboard' }));
    const box = screen.getByRole('textbox', { name: 'Step text' });
    fireEvent.change(box, { target: { value: 'Check the logs' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(h.onSaveText).toHaveBeenCalledWith('f1', 'Check the logs');

    fireEvent.click(screen.getByRole('button', { name: 'Remove check' }));
    expect(h.onRemove).toHaveBeenCalledWith('f1');

    fireEvent.click(screen.getByRole('button', { name: 'Add a check' }));
    const add = screen.getByRole('textbox', { name: 'New check' });
    fireEvent.change(add, { target: { value: 'Smoke test' } });
    fireEvent.keyDown(add, { key: 'Enter' });
    expect(h.onAdd).toHaveBeenCalledWith('Smoke test');
  });

  it('Escape discards the draft even if a blur follows', () => {
    const h = setup([], false);
    fireEvent.click(screen.getByRole('button', { name: 'Add a check' }));
    const add = screen.getByRole('textbox', { name: 'New check' });
    fireEvent.change(add, { target: { value: 'Discard me' } });
    fireEvent.keyDown(add, { key: 'Escape' });
    fireEvent.blur(add);
    expect(h.onAdd).not.toHaveBeenCalled();
  });

  it('ticks send done when unlocked', () => {
    const h = setup([followup()], false);
    const box = screen.getByRole('checkbox', { name: 'Check the dashboard' });
    expect(box).toBeEnabled();
    fireEvent.click(box);
    expect(h.onToggle).toHaveBeenCalledWith('f1', true);
  });

  it('labels card refs by number', () => {
    setup([followup({ cards: [{ project: 'recipe-box', card: 'RECIPEBO-002' }] })], false);
    const label = screen.getByText('002');
    expect(label).toHaveAttribute('title', 'recipe-box/RECIPEBO-002');
  });

  it('shows only an add control when there are none', () => {
    setup([], false);
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0);
    expect(screen.getByRole('button', { name: 'Add a check' })).toBeInTheDocument();
  });
});
