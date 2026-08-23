import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
import { api } from '../../../api/client';
import type { Card, ProjectConfig } from '../../../types';
import { MetadataRelated } from './MetadataRelated';

function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: 'TEST-001',
    title: 'Main card',
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

const config: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 5,
  states: ['todo', 'in_progress', 'done'],
  types: ['task', 'subtask'],
  priorities: ['medium'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
};

// All fixture cards are non-terminal - CardPicker filters out done/not_planned.
const depCard = makeCard({ id: 'TEST-002', title: 'Existing dependency', state: 'todo' });
const subtaskCard = makeCard({ id: 'TEST-003', title: 'Existing subtask', state: 'todo' });
const unrelatedCard = makeCard({ id: 'TEST-004', title: 'Unrelated card', state: 'in_progress' });
const selfCard = makeCard({
  id: 'TEST-001',
  title: 'Main card',
  depends_on: ['TEST-002'],
  subtasks: ['TEST-003'],
});

const cardsById: Record<string, Card> = {
  'TEST-001': selfCard,
  'TEST-002': depCard,
  'TEST-003': subtaskCard,
  'TEST-004': unrelatedCard,
};

const boardCards = [selfCard, depCard, subtaskCard, unrelatedCard];

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(api, 'getCard').mockImplementation(
    async (_project: string, id: string) => cardsById[id] ?? makeCard({ id, title: id }),
  );
});

describe('MetadataRelated - dependency picker', () => {
  it('shows the picker on click and omits the card itself, its deps, and its subtasks while showing an unrelated card', async () => {
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: '+ add dependency' }));

    fireEvent.change(screen.getByLabelText('Filter cards by id or title'), {
      target: { value: 'TEST' },
    });

    const list = await screen.findByRole('list', { name: 'Card results' });
    expect(within(list).getByText('TEST-004')).toBeInTheDocument();
    expect(within(list).queryByText('TEST-001')).not.toBeInTheDocument();
    expect(within(list).queryByText('TEST-002')).not.toBeInTheDocument();
    expect(within(list).queryByText('TEST-003')).not.toBeInTheDocument();
  });

  it('calls onDependsOnChange with the existing list plus the picked id, in that order', async () => {
    const onDependsOnChange = vi.fn().mockResolvedValue(undefined);
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: '+ add dependency' }));
    fireEvent.change(screen.getByPlaceholderText('Filter cards by id or title'), {
      target: { value: 'TEST' },
    });

    const list = await screen.findByRole('list', { name: 'Card results' });
    fireEvent.click(within(list).getByText('TEST-004'));

    expect(onDependsOnChange).toHaveBeenCalledWith(['TEST-002', 'TEST-004']);
  });

  it('calls onDependsOnChange with the id removed when the remove control is clicked', async () => {
    const onDependsOnChange = vi.fn().mockResolvedValue(undefined);
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-002' }));
    expect(onDependsOnChange).toHaveBeenCalledWith([]);
  });

  it('disables the trigger and every remove button while the save promise is pending, and re-enables once it settles', async () => {
    let resolveSave: () => void = () => {};
    const pending = new Promise<void>((resolve) => { resolveSave = resolve; });
    const onDependsOnChange = vi.fn().mockReturnValue(pending);
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-002' }));

    expect(screen.getByRole('button', { name: '+ add dependency' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Remove dependency TEST-002' })).toBeDisabled();

    resolveSave();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '+ add dependency' })).not.toBeDisabled();
    });
    expect(screen.getByRole('button', { name: 'Remove dependency TEST-002' })).not.toBeDisabled();
  });

  it('re-enables after rejection and computes the next click from the original card.depends_on, not an accumulated copy', async () => {
    const multiDepCard = makeCard({
      id: 'TEST-001',
      title: 'Main card',
      depends_on: ['TEST-002', 'TEST-004', 'TEST-005'],
    });
    const onDependsOnChange = vi.fn().mockRejectedValueOnce(new Error('409'));
    render(
      <MetadataRelated
        card={multiDepCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-002' }));
    expect(onDependsOnChange).toHaveBeenNthCalledWith(1, ['TEST-004', 'TEST-005']);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Remove dependency TEST-004' })).not.toBeDisabled();
    });

    // The rejected save never touched card.depends_on - the prop is still
    // the original three-item list, so removing a different id recomputes
    // from it rather than from the (never-applied) first attempt's result.
    onDependsOnChange.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-004' }));
    expect(onDependsOnChange).toHaveBeenNthCalledWith(2, ['TEST-002', 'TEST-005']);
  });

  it('after resolution, computes the next click from the updated card.depends_on prop', async () => {
    const multiDepCard = makeCard({
      id: 'TEST-001',
      title: 'Main card',
      depends_on: ['TEST-002', 'TEST-004', 'TEST-005'],
    });
    const onDependsOnChange = vi.fn().mockResolvedValue(undefined);
    const { rerender } = render(
      <MetadataRelated
        card={multiDepCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-002' }));
    expect(onDependsOnChange).toHaveBeenNthCalledWith(1, ['TEST-004', 'TEST-005']);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Remove dependency TEST-004' })).not.toBeDisabled();
    });

    // Simulates updateCardLocally having applied the server response before
    // the controls re-enabled.
    const updatedCard = makeCard({
      id: 'TEST-001',
      title: 'Main card',
      depends_on: ['TEST-004', 'TEST-005'],
    });
    rerender(
      <MetadataRelated
        card={updatedCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={onDependsOnChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove dependency TEST-004' }));
    expect(onDependsOnChange).toHaveBeenNthCalledWith(2, ['TEST-005']);
  });

  it('renders neither the add button nor remove controls when workerAttached, but keeps the section when deps exist', async () => {
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(await screen.findByRole('heading', { level: 4, name: /Depends on/ })).toBeInTheDocument();
    expect(screen.getByText('TEST-002')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '+ add dependency' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove dependency TEST-002' })).not.toBeInTheDocument();
  });

  it('sets aria-haspopup="dialog" on the trigger and role="dialog" with an accessible name on the popover', () => {
    render(
      <MetadataRelated
        card={selfCard}
        config={config}
        workerAttached={false}
        cards={boardCards}
        onSubtaskClick={vi.fn()}
        onDependsOnChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );
    const trigger = screen.getByRole('button', { name: '+ add dependency' });
    expect(trigger).toHaveAttribute('aria-haspopup', 'dialog');

    fireEvent.click(trigger);
    expect(screen.getByRole('dialog', { name: 'Add dependency' })).toBeInTheDocument();
  });
});
