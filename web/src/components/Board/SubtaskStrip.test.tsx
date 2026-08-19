import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SubtaskStrip, SubtaskPeekList } from './SubtaskStrip';
import { hasUnmetDeps, stripSegClass } from '../../lib/chip';
import type { Card } from '../../types';

function makeSub(id: string, overrides: Partial<Card> = {}): Card {
  return {
    id,
    title: `Subtask ${id}`,
    project: 'test',
    type: 'subtask',
    state: 'todo',
    priority: 'low',
    parent: 'TEST-001',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  };
}

describe('stripSegClass', () => {
  it('maps known states to their phase-seg class', () => {
    expect(stripSegClass('todo')).toBe('phase-seg-todo');
    expect(stripSegClass('in_progress')).toBe('phase-seg-in_progress');
    expect(stripSegClass('review')).toBe('phase-seg-review');
    expect(stripSegClass('done')).toBe('phase-seg-done');
    expect(stripSegClass('blocked')).toBe('phase-seg-blocked');
    expect(stripSegClass('stalled')).toBe('phase-seg-stalled');
    expect(stripSegClass('hitl')).toBe('phase-seg-hitl');
    expect(stripSegClass('not_planned')).toBe('phase-seg-not_planned');
  });

  it('falls back to phase-seg-todo for unknown states', () => {
    expect(stripSegClass('custom_state')).toBe('phase-seg-todo');
  });
});

describe('hasUnmetDeps', () => {
  it('is false without depends_on', () => {
    expect(hasUnmetDeps(makeSub('TEST-002'))).toBe(false);
  });

  it('is false when dependencies are met', () => {
    expect(hasUnmetDeps(makeSub('TEST-002', { depends_on: ['TEST-009'], dependencies_met: true }))).toBe(false);
  });

  it('is true when depends_on is set and dependencies_met is falsy', () => {
    expect(hasUnmetDeps(makeSub('TEST-002', { depends_on: ['TEST-009'], dependencies_met: false }))).toBe(true);
    // Go omitempty drops false, so the field may be absent entirely.
    expect(hasUnmetDeps(makeSub('TEST-002', { depends_on: ['TEST-009'] }))).toBe(true);
  });
});

describe('SubtaskStrip', () => {
  const three = [
    makeSub('TEST-002'),
    makeSub('TEST-003', { state: 'in_progress' }),
    makeSub('TEST-004', { state: 'done' }),
  ];

  it('renders one state-colored segment per subtask with an id · state tooltip', () => {
    const { container } = render(<SubtaskStrip subtasks={three} onToggle={vi.fn()} />);
    const segs = container.querySelectorAll('.phase-seg');
    expect(segs).toHaveLength(3);
    expect(segs[0].className).toContain('phase-seg-todo');
    expect(segs[1].className).toContain('phase-seg-in_progress');
    expect(segs[2].className).toContain('phase-seg-done');
    expect(screen.getByTitle('TEST-003 · in_progress')).toBeInTheDocument();
  });

  it('unknown subtask state falls back to the todo segment color', () => {
    const { container } = render(
      <SubtaskStrip subtasks={[makeSub('TEST-002', { state: 'weird' })]} onToggle={vi.fn()} />,
    );
    expect(container.querySelector('.phase-seg')!.className).toContain('phase-seg-todo');
  });

  it('is a button labeled with the subtask count that reflects expansion and toggles', () => {
    const onToggle = vi.fn();
    const { rerender } = render(<SubtaskStrip subtasks={three} expanded={false} onToggle={onToggle} />);
    const strip = screen.getByRole('button', { name: '3 subtasks' });
    expect(strip).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(strip);
    expect(onToggle).toHaveBeenCalledOnce();
    rerender(<SubtaskStrip subtasks={three} expanded onToggle={onToggle} />);
    expect(screen.getByRole('button', { name: '3 subtasks' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('pluralizes the label correctly for a single subtask', () => {
    render(<SubtaskStrip subtasks={[makeSub('TEST-002')]} onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: '1 subtask' })).toBeInTheDocument();
  });

  it('renders a plain non-interactive strip when interactive is false', () => {
    const { container } = render(<SubtaskStrip subtasks={three} interactive={false} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(container.querySelectorAll('.phase-seg')).toHaveLength(3);
  });

  it('adds phase-strip--tall class when tall prop is true', () => {
    render(<SubtaskStrip subtasks={three} tall onToggle={vi.fn()} />);
    const strip = screen.getByRole('button', { name: '3 subtasks' });
    expect(strip.className).toContain('phase-strip--tall');
  });

  it('tall prop is silently ignored in static (non-interactive) mode', () => {
    const { container } = render(<SubtaskStrip subtasks={three} interactive={false} tall />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    const div = container.querySelector('.phase-strip');
    expect(div).toBeInTheDocument();
    expect(div!.className).not.toContain('phase-strip--tall');
  });
});

describe('SubtaskPeekList', () => {
  it('renders one row per subtask with state chip, short id, and title', () => {
    render(
      <SubtaskPeekList
        subtasks={[makeSub('TEST-002', { state: 'in_progress' })]}
        onOpen={vi.fn()}
      />,
    );
    const row = screen.getByTitle('Subtask TEST-002');
    expect(row.className).toContain('peek-row');
    expect(screen.getByText('in progress')).toBeInTheDocument();
    expect(screen.getByText('002')).toBeInTheDocument();
    expect(screen.getByText('Subtask TEST-002')).toBeInTheDocument();
  });

  it('clicking a row calls onOpen with the subtask id', () => {
    const onOpen = vi.fn();
    render(<SubtaskPeekList subtasks={[makeSub('TEST-002'), makeSub('TEST-003')]} onOpen={onOpen} />);
    fireEvent.click(screen.getByTitle('Subtask TEST-003'));
    expect(onOpen).toHaveBeenCalledOnce();
    expect(onOpen).toHaveBeenCalledWith('TEST-003');
  });

  it('dependency-blocked rows get the red tint modifier and tooltip; blocked-state rows do not', () => {
    render(
      <SubtaskPeekList
        subtasks={[
          makeSub('TEST-002', { depends_on: ['TEST-009'], dependencies_met: false }),
          makeSub('TEST-003', { state: 'blocked' }),
        ]}
        onOpen={vi.fn()}
      />,
    );
    const depBlocked = screen.getByTitle('TEST-002: blocked by dependencies');
    expect(depBlocked.className).toContain('peek-row--dep-blocked');
    const stateBlocked = screen.getByTitle('Subtask TEST-003');
    expect(stateBlocked.className).not.toContain('peek-row--dep-blocked');
  });

  it('agent-held non-stalled rows get the active modifier and avatar; stalled rows do not', () => {
    render(
      <SubtaskPeekList
        subtasks={[
          makeSub('TEST-002', { state: 'in_progress', assigned_agent: 'claude-sonnet-worker' }),
          makeSub('TEST-003', { state: 'stalled', assigned_agent: 'claude-sonnet-worker' }),
        ]}
        onOpen={vi.fn()}
      />,
    );
    const active = screen.getByTitle('Subtask TEST-002');
    expect(active.className).toContain('peek-row--agent');
    expect(active.querySelector('.agent-avatar')).not.toBeNull();
    expect(screen.getByTitle('Subtask TEST-003').className).not.toContain('peek-row--agent');
  });
});
