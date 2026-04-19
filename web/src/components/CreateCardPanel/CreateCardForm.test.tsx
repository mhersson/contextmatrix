import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { useState } from 'react';
import { CreateCardForm } from './CreateCardForm';
import type { ProjectConfig, Card } from '../../types';

// Mock MDEditor to avoid complex editor DOM setup in tests
vi.mock('@uiw/react-md-editor', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="md-editor"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

// Mock useTheme to avoid ThemeProvider requirement
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', palette: 'everforest', toggleTheme: vi.fn() }),
}));

const baseConfig: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 1,
  states: ['todo', 'in_progress', 'done'],
  types: ['task', 'bug', 'feature'],
  priorities: ['low', 'medium', 'high'],
  transitions: {},
  templates: {
    task: '## Task template\n',
    // 'bug' has no template — used for the no-template tests
  },
};

const noCards: Card[] = [];

// Controlled wrapper that tracks state changes and captures setter calls
function ControlledForm({
  initialBody = '',
  initialBodyDirty = false,
  initialType = 'task',
  config = baseConfig,
  onBodyChange,
  onBodyDirtyChange,
  onTypeChange,
}: {
  initialBody?: string;
  initialBodyDirty?: boolean;
  initialType?: string;
  config?: ProjectConfig;
  onBodyChange?: (v: string) => void;
  onBodyDirtyChange?: (v: boolean) => void;
  onTypeChange?: (v: string) => void;
}) {
  const [type, setType] = useState(initialType);
  const [body, setBody] = useState(initialBody);
  const [bodyDirty, setBodyDirty] = useState(initialBodyDirty);

  return (
    <CreateCardForm
      title=""
      setTitle={vi.fn()}
      type={type}
      setType={(v) => { setType(v); onTypeChange?.(v); }}
      priority="medium"
      setPriority={vi.fn()}
      labels={[]}
      setLabels={vi.fn()}
      parent=""
      setParent={vi.fn()}
      body={body}
      setBody={(v) => { setBody(v); onBodyChange?.(v); }}
      config={config}
      cards={noCards}
      bodyDirty={bodyDirty}
      setBodyDirty={(v) => { setBodyDirty(v); onBodyDirtyChange?.(v); }}
      autonomous={false}
      setAutonomous={vi.fn()}
      useOpusOrchestrator={false}
      setUseOpusOrchestrator={vi.fn()}
      featureBranch={false}
      setFeatureBranch={vi.fn()}
      createPR={false}
      setCreatePR={vi.fn()}
      baseBranch=""
      onBaseBranchChange={vi.fn()}
      branches={[]}
    />
  );
}

describe('CreateCardForm — handleTypeChange template behavior', () => {
  beforeEach(() => {
    // Reset window.confirm mock before each test
    vi.restoreAllMocks();
  });

  it('switching to a type WITH a template populates the body when body is not dirty', () => {
    const onBodyChange = vi.fn();
    // Start with 'bug' (no template) so we can switch to 'task' (has template)
    render(
      <ControlledForm
        initialType="bug"
        initialBody=""
        initialBodyDirty={false}
        onBodyChange={onBodyChange}
      />,
    );

    // The Type select is the first combobox; Priority is the second
    const [select] = screen.getAllByRole('combobox');
    fireEvent.change(select, { target: { value: 'task' } });

    // Body should be populated with the task template
    expect(onBodyChange).toHaveBeenCalledWith('## Task template\n');

    // Editor should reflect the template content
    const editor = screen.getByTestId('md-editor');
    expect(editor).toHaveValue('## Task template\n');
  });

  it('switching to a type WITH NO template clears the body when body is not dirty', () => {
    const onBodyChange = vi.fn();
    // Start with 'task' (has template, body pre-populated, not dirty)
    render(
      <ControlledForm
        initialType="task"
        initialBody="## Task template\n"
        initialBodyDirty={false}
        onBodyChange={onBodyChange}
      />,
    );

    // The Type select is the first combobox; Priority is the second
    const [select] = screen.getAllByRole('combobox');
    fireEvent.change(select, { target: { value: 'bug' } });

    // Body should be cleared since 'bug' has no template and body is not dirty
    expect(onBodyChange).toHaveBeenCalledWith('');

    // Editor should be empty
    const editor = screen.getByTestId('md-editor');
    expect(editor).toHaveValue('');
  });

  it('switching to a type WITH NO template does NOT clear the body when body IS dirty', () => {
    const onBodyChange = vi.fn();
    const userContent = 'my custom description';

    render(
      <ControlledForm
        initialType="task"
        initialBody={userContent}
        initialBodyDirty={true}
        onBodyChange={onBodyChange}
      />,
    );

    // The Type select is the first combobox; Priority is the second
    const [select] = screen.getAllByRole('combobox');
    fireEvent.change(select, { target: { value: 'bug' } });

    // setBody should NOT have been called (body preserved)
    expect(onBodyChange).not.toHaveBeenCalled();

    // Editor should still show the user's content
    const editor = screen.getByTestId('md-editor');
    expect(editor).toHaveValue(userContent);
  });

  it('switching to a type WITH a template when body IS dirty prompts the user', () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(
      <ControlledForm
        initialType="bug"
        initialBody="my custom description"
        initialBodyDirty={true}
      />,
    );

    // The Type select is the first combobox; Priority is the second
    const [select] = screen.getAllByRole('combobox');
    fireEvent.change(select, { target: { value: 'task' } });

    expect(confirmSpy).toHaveBeenCalledOnce();
    expect(confirmSpy).toHaveBeenCalledWith('Load template for "task"? This will replace the current body.');
  });
});
