import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { StateTransitionEditor } from './StateTransitionEditor';

const states = ['todo', 'in_progress', 'done'];
const transitions = { todo: ['in_progress'], in_progress: ['done', 'todo'], done: [] };

describe('StateTransitionEditor matrix', () => {
  it('renders a from/to cell for every ordered pair of distinct states', () => {
    render(<StateTransitionEditor states={states} transitions={transitions} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'todo to in_progress' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'todo to done' })).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'in_progress to todo' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByRole('button', { name: 'todo to todo' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button')).toHaveLength(states.length * (states.length - 1));
  });

  it('labels rows and columns with the state names', () => {
    render(<StateTransitionEditor states={states} transitions={transitions} onChange={vi.fn()} />);
    expect(screen.getAllByRole('columnheader', { name: 'done' })).toHaveLength(1);
    expect(screen.getAllByRole('rowheader', { name: 'done' })).toHaveLength(1);
  });

  it('clicking an allowed cell removes the transition', () => {
    const onChange = vi.fn();
    render(<StateTransitionEditor states={states} transitions={transitions} onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'in_progress to todo' }));
    expect(onChange).toHaveBeenCalledWith({ ...transitions, in_progress: ['done'] });
  });

  it('clicking a blocked cell adds the transition', () => {
    const onChange = vi.fn();
    render(<StateTransitionEditor states={states} transitions={transitions} onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'done to todo' }));
    expect(onChange).toHaveBeenCalledWith({ ...transitions, done: ['todo'] });
  });
});
