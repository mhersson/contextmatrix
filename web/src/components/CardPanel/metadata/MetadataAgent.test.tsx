import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import type { Card } from '../../../types';
import { MetadataAgent } from './MetadataAgent';

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

describe('MetadataAgent — runner attached + agent branch', () => {
  it('shows the assigned agent ID and heartbeat when runner is attached', () => {
    render(
      <MetadataAgent
        card={makeCard({
          assigned_agent: 'human:alice',
          last_heartbeat: '2026-01-01T00:00:00Z',
          state: 'in_progress',
          runner_status: 'running',
        })}
        currentAgentId="human:alice"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByText('human:alice')).toBeInTheDocument();
    expect(screen.getByText(/heartbeat/)).toBeInTheDocument();
  });

  it('shows the Release button only when current agent is human', () => {
    const { rerender } = render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'in_progress', runner_status: 'running' })}
        currentAgentId="human:alice"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Release' })).toBeInTheDocument();

    // Non-human current agent → Release hidden.
    rerender(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'bot-1', state: 'in_progress', runner_status: 'running' })}
        currentAgentId="bot-1"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Release' })).not.toBeInTheDocument();
  });

  it('hides the Release button when there is no current agent set in the browser', () => {
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'in_progress', runner_status: 'running' })}
        currentAgentId={null}
        runnerAttached
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Release' })).not.toBeInTheDocument();
  });
});

describe('MetadataAgent — Release ConfirmModal flow', () => {
  it('opens ConfirmModal on Release click without calling onRelease', () => {
    const onRelease = vi.fn();
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'in_progress', runner_status: 'running' })}
        currentAgentId="human:alice"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={onRelease}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    expect(onRelease).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText(/Release claim held by human:alice/)).toBeInTheDocument();
  });

  it('calls onRelease once when the modal Release button is clicked', () => {
    const onRelease = vi.fn();
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'in_progress', runner_status: 'running' })}
        currentAgentId="human:alice"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={onRelease}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    // The modal confirm button carries the confirmLabel "Release" — there are
    // now two buttons with name "Release" (the one in the agent row and the
    // modal confirm). getAllByRole → the last one is the modal confirm.
    const releaseButtons = screen.getAllByRole('button', { name: 'Release' });
    fireEvent.click(releaseButtons[releaseButtons.length - 1]);
    expect(onRelease).toHaveBeenCalledOnce();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('does not call onRelease when the modal is cancelled', () => {
    const onRelease = vi.fn();
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'in_progress', runner_status: 'running' })}
        currentAgentId="human:alice"
        runnerAttached
        onClaim={vi.fn()}
        onRelease={onRelease}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onRelease).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('MetadataAgent — released (previously claimed) branch', () => {
  it('shows "released · no active claim" when an agent is recorded but runner not attached', () => {
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: 'human:alice', state: 'todo' })}
        currentAgentId="human:alice"
        runnerAttached={false}
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByText(/released · no active claim/)).toBeInTheDocument();
    expect(screen.getByText(/last: human:alice/)).toBeInTheDocument();
  });

  it('treats a card with last_heartbeat but no assigned_agent as released', () => {
    render(
      <MetadataAgent
        card={makeCard({
          assigned_agent: undefined,
          last_heartbeat: '2026-01-01T00:00:00Z',
          state: 'todo',
        })}
        currentAgentId="human:alice"
        runnerAttached={false}
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByText(/released · no active claim/)).toBeInTheDocument();
    // last-claimer hint is only rendered when assigned_agent is still set, so
    // a heartbeat-only card should not surface the "last: …" line.
    expect(screen.queryByText(/^last:/)).not.toBeInTheDocument();
  });
});

describe('MetadataAgent — fresh unassigned todo branch', () => {
  it('shows "unassigned · runner ready" and the Just claim button', () => {
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: undefined, last_heartbeat: undefined, state: 'todo' })}
        currentAgentId={null}
        runnerAttached={false}
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByText('unassigned')).toBeInTheDocument();
    expect(screen.getByText(/runner ready/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Just claim' })).toBeInTheDocument();
  });

  it('Just claim click fires onClaim exactly once', () => {
    const onClaim = vi.fn();
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: undefined, last_heartbeat: undefined, state: 'todo' })}
        currentAgentId={null}
        runnerAttached={false}
        onClaim={onClaim}
        onRelease={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Just claim' }));
    expect(onClaim).toHaveBeenCalledOnce();
  });

  it('hides the Just claim button when the runner is attached (no claim race)', () => {
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: undefined, state: 'todo' })}
        currentAgentId={null}
        runnerAttached
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Just claim' })).not.toBeInTheDocument();
  });

  it('omits the "runner ready" hint for non-todo cards (state-dependent label)', () => {
    render(
      <MetadataAgent
        card={makeCard({ assigned_agent: undefined, last_heartbeat: undefined, state: 'review' })}
        currentAgentId={null}
        runnerAttached={false}
        onClaim={vi.fn()}
        onRelease={vi.fn()}
      />,
    );
    expect(screen.getByText('unassigned')).toBeInTheDocument();
    expect(screen.queryByText(/runner ready/)).not.toBeInTheDocument();
  });
});
