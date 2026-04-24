import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import type { Card } from '../../types';
import { DangerZoneTab } from './CardPanelDangerZone';

function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: 'TEST-007',
    title: 'Test',
    project: 'test',
    type: 'task',
    state: 'todo',
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  };
}

describe('DangerZoneTab — enabled delete flow', () => {
  it('opens the ConfirmModal on first click (does not call onDelete yet)', () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <DangerZoneTab
        card={makeCard()}
        canDelete
        deleteTooltip="Delete TEST-007"
        isDeleting={false}
        onDelete={onDelete}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    expect(onDelete).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Delete card TEST-007?')).toBeInTheDocument();
  });

  it('invokes onDelete when the modal Delete button is clicked', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <DangerZoneTab
        card={makeCard()}
        canDelete
        deleteTooltip="Delete TEST-007"
        isDeleting={false}
        onDelete={onDelete}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    });
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it('cancels cleanly without invoking onDelete', () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <DangerZoneTab
        card={makeCard()}
        canDelete
        deleteTooltip="Delete TEST-007"
        isDeleting={false}
        onDelete={onDelete}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onDelete).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('DangerZoneTab — disabled states', () => {
  it('disables the Delete button and shows reason when an agent holds the claim', () => {
    render(
      <DangerZoneTab
        card={makeCard({ assigned_agent: 'human:someone', state: 'todo' })}
        canDelete={false}
        deleteTooltip="Claimed — cannot delete"
        isDeleting={false}
        onDelete={vi.fn()}
      />,
    );
    const button = screen.getByRole('button', { name: 'Delete card' });
    expect(button).toBeDisabled();
    expect(screen.getByText(/An agent has an active claim/)).toBeInTheDocument();
  });

  it('disables the Delete button and explains when state is not todo/not_planned', () => {
    render(
      <DangerZoneTab
        card={makeCard({ state: 'in_progress' })}
        canDelete={false}
        deleteTooltip="State blocks delete"
        isDeleting={false}
        onDelete={vi.fn()}
      />,
    );
    const button = screen.getByRole('button', { name: 'Delete card' });
    expect(button).toBeDisabled();
    expect(screen.getByText(/current state is in progress/)).toBeInTheDocument();
  });

  it('ignores clicks when disabled (no modal opens)', () => {
    const onDelete = vi.fn();
    render(
      <DangerZoneTab
        card={makeCard({ state: 'review' })}
        canDelete={false}
        deleteTooltip="State blocks delete"
        isDeleting={false}
        onDelete={onDelete}
      />,
    );
    const button = screen.getByRole('button', { name: 'Delete card' });
    fireEvent.click(button);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(onDelete).not.toHaveBeenCalled();
  });

  it('renders "Deleting…" while a delete is in flight', () => {
    render(
      <DangerZoneTab
        card={makeCard()}
        canDelete
        deleteTooltip="Delete TEST-007"
        isDeleting
        onDelete={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Delete card' })).toHaveTextContent('Deleting…');
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });
});

describe('DangerZoneTab — force release placeholder', () => {
  it('renders the force-release card as disabled with reason copy', () => {
    render(
      <DangerZoneTab
        card={makeCard()}
        canDelete
        deleteTooltip="Delete TEST-007"
        isDeleting={false}
        onDelete={vi.fn()}
      />,
    );
    const forceRelease = screen.getByRole('button', { name: /Force release/ });
    expect(forceRelease).toBeDisabled();
    expect(screen.getByText(/Only available when the runner is unresponsive/)).toBeInTheDocument();
  });
});
