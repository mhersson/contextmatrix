import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CardPanel } from './CardPanel';
import type { Card, ProjectConfig } from '../../types';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', toggleTheme: vi.fn() }),
}));

vi.mock('@uiw/react-md-editor', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="md-editor"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

vi.mock('../../api/client', () => ({
  api: {
    fetchBranches: vi.fn().mockResolvedValue([]),
  },
}));

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
  autonomous: false,
  feature_branch: false,
  create_pr: false,
};

const config: ProjectConfig = {
  name: 'Test',
  prefix: 'TEST',
  next_id: 2,
  states: ['todo', 'in_progress', 'done'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: { todo: ['in_progress'], in_progress: ['done'] },
  remote_execution: { enabled: true },
};

function makeProps(overrides?: Partial<Parameters<typeof CardPanel>[0]>) {
  return {
    card: baseCard,
    config,
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    onClaim: vi.fn().mockResolvedValue(undefined),
    onRelease: vi.fn().mockResolvedValue(undefined),
    onSubtaskClick: vi.fn(),
    currentAgentId: null,
    onPromptAgentId: vi.fn().mockReturnValue(null),
    onRunCard: vi.fn().mockResolvedValue(undefined),
    onStopCard: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function renderWithTheme(ui: React.ReactElement) {
  return render(ui);
}

describe('CardPanel — Run Now save-before-run ordering', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('calls onSave before onRunCard when card is dirty', async () => {
    const calls: string[] = [];
    const onSave = vi.fn(async () => { calls.push('save'); });
    const onRunCard = vi.fn(async () => { calls.push('run'); });

    renderWithTheme(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    // Make the card dirty by changing the title
    const titleInput = screen.getByDisplayValue('Test card');
    fireEvent.change(titleInput, { target: { value: 'Dirty title' } });

    // Click Run Now
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));
    });

    expect(calls).toEqual(['save', 'run']);
  });

  it('does NOT call onSave when card is not dirty', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);

    renderWithTheme(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    // No changes made — card is clean
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));
    });

    expect(onSave).not.toHaveBeenCalled();
    expect(onRunCard).toHaveBeenCalledOnce();
  });

});
