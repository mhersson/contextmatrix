import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import type { ComponentProps } from 'react';
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

function renderTab(overrides: Partial<ComponentProps<typeof DangerZoneTab>> = {}) {
  const props: ComponentProps<typeof DangerZoneTab> = {
    card: makeCard(),
    canDelete: false,
    deleteTooltip: '',
    isDeleting: false,
    onDelete: vi.fn().mockResolvedValue(undefined),
    canForceRelease: false,
    isForceReleasing: false,
    onForceRelease: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  render(<DangerZoneTab {...props} />);
  return props;
}

describe('DangerZoneTab - enabled delete flow', () => {
  it('opens the ConfirmModal on first click (does not call onDelete yet)', () => {
    const { onDelete } = renderTab({ canDelete: true, deleteTooltip: 'Delete TEST-007' });
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    expect(onDelete).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Delete card TEST-007?')).toBeInTheDocument();
  });

  it('invokes onDelete when the modal Delete button is clicked', async () => {
    const { onDelete } = renderTab({ canDelete: true, deleteTooltip: 'Delete TEST-007' });
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    });
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it('cancels cleanly without invoking onDelete', () => {
    const { onDelete } = renderTab({ canDelete: true, deleteTooltip: 'Delete TEST-007' });
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onDelete).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('DangerZoneTab - disabled states', () => {
  it('disables the Delete button and shows reason when an agent holds the claim', () => {
    renderTab({
      card: makeCard({ assigned_agent: 'human:someone', state: 'todo' }),
      deleteTooltip: 'Claimed - cannot delete',
    });
    const button = screen.getByRole('button', { name: 'Delete card' });
    expect(button).toBeDisabled();
    expect(screen.getByText(/An agent has an active claim/)).toBeInTheDocument();
  });

  it('disables the Delete button and explains when state is not todo/not_planned', () => {
    renderTab({
      card: makeCard({ state: 'in_progress' }),
      deleteTooltip: 'State blocks delete',
    });
    const button = screen.getByRole('button', { name: 'Delete card' });
    expect(button).toBeDisabled();
    expect(screen.getByText(/current state is in progress/)).toBeInTheDocument();
  });

  it('ignores clicks when disabled (no modal opens)', () => {
    const { onDelete } = renderTab({
      card: makeCard({ state: 'review' }),
      deleteTooltip: 'State blocks delete',
    });
    const button = screen.getByRole('button', { name: 'Delete card' });
    fireEvent.click(button);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(onDelete).not.toHaveBeenCalled();
  });

  it('renders "Deleting…" while a delete is in flight', () => {
    renderTab({ canDelete: true, deleteTooltip: 'Delete TEST-007', isDeleting: true });
    expect(screen.getByRole('button', { name: 'Delete card' })).toHaveTextContent('Deleting…');
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });
});

describe('DangerZoneTab - force release', () => {
  it('disables the button and shows the reason when no claim exists', () => {
    renderTab();
    expect(screen.getByRole('button', { name: 'Force-release agent claim' })).toBeDisabled();
    expect(screen.getByText(/No agent claim to release/)).toBeInTheDocument();
  });

  it('ignores clicks when disabled (no modal opens)', () => {
    const { onForceRelease } = renderTab();
    fireEvent.click(screen.getByRole('button', { name: 'Force-release agent claim' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(onForceRelease).not.toHaveBeenCalled();
  });

  it('opens the ConfirmModal naming the holding agent on first click', () => {
    const { onForceRelease } = renderTab({
      card: makeCard({ assigned_agent: 'agent-42', state: 'in_progress' }),
      canForceRelease: true,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Force-release agent claim' }));
    expect(onForceRelease).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Force-release claim on TEST-007?')).toBeInTheDocument();
    expect(screen.getByText(/agent-42/)).toBeInTheDocument();
  });

  it('invokes onForceRelease when confirmed', async () => {
    const { onForceRelease } = renderTab({
      card: makeCard({ assigned_agent: 'agent-42', state: 'in_progress' }),
      canForceRelease: true,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Force-release agent claim' }));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Force release' }));
    });
    expect(onForceRelease).toHaveBeenCalledOnce();
  });

  it('cancels cleanly without invoking onForceRelease', () => {
    const { onForceRelease } = renderTab({
      card: makeCard({ assigned_agent: 'agent-42', state: 'in_progress' }),
      canForceRelease: true,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Force-release agent claim' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onForceRelease).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('hides the no-claim reason and renders "Releasing…" while in flight', () => {
    renderTab({
      card: makeCard({ assigned_agent: 'agent-42', state: 'in_progress' }),
      canForceRelease: true,
      isForceReleasing: true,
    });
    const button = screen.getByRole('button', { name: 'Force-release agent claim' });
    expect(button).toHaveTextContent('Releasing…');
    expect(button).toBeDisabled();
    expect(screen.queryByText(/No agent claim to release/)).not.toBeInTheDocument();
  });
});
