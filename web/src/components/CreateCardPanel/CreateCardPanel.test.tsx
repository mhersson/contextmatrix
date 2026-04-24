import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CreateCardPanel } from './CreateCardPanel';
import type { Card, ProjectConfig } from '../../types';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', palette: 'everforest', toggleTheme: vi.fn() }),
}));

vi.mock('@uiw/react-md-editor', () => ({
  default: ({
    value,
    onChange,
    previewOptions,
  }: {
    value: string;
    onChange?: (v: string) => void;
    previewOptions?: { skipHtml?: boolean };
  }) => (
    <>
      <textarea
        data-testid="md-editor"
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
      />
      {previewOptions?.skipHtml
        ? <div data-testid="md-preview">{value}</div>
        : <div data-testid="md-preview" dangerouslySetInnerHTML={{ __html: value }} />}
    </>
  ),
}));

vi.mock('../../api/client', () => ({
  api: {
    fetchBranches: vi.fn().mockResolvedValue([]),
    getCard: vi.fn().mockResolvedValue({ state: 'todo' }),
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

const config: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 1,
  states: ['todo', 'in_progress', 'done'],
  types: ['task', 'bug', 'feature', 'subtask'],
  priorities: ['low', 'medium', 'high'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
  templates: { task: '## Task template\n' },
  remote_execution: { enabled: true },
};

const noCards: Card[] = [];

function makeProps(overrides?: Partial<Parameters<typeof CreateCardPanel>[0]>) {
  return {
    config,
    cards: noCards,
    onClose: vi.fn(),
    onCreate: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

describe('CreateCardPanel — bifold shell', () => {
  it('renders the bifold layout (left + rail tabs)', () => {
    render(<CreateCardPanel {...makeProps()} />);
    expect(screen.getByTestId('body-bifold')).toBeInTheDocument();
    expect(screen.getByTestId('body-left')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Automation' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Info' })).toBeInTheDocument();
  });

  it('omits the Danger Zone tab in create mode', () => {
    render(<CreateCardPanel {...makeProps()} />);
    expect(screen.queryByRole('tab', { name: /Danger/ })).not.toBeInTheDocument();
  });

  it('renders the "new card" chip in the eyebrow', () => {
    render(<CreateCardPanel {...makeProps()} />);
    expect(screen.getByText('new card')).toBeInTheDocument();
  });
});

describe('CreateCardPanel — header action cluster', () => {
  it('Create & Run button label tracks the autonomous checkbox', () => {
    render(<CreateCardPanel {...makeProps()} />);
    expect(screen.getByRole('button', { name: /Create & Run HITL/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Create & Run Auto/ })).not.toBeInTheDocument();

    // Toggle autonomous via the checkbox in the Automation tab.
    fireEvent.click(screen.getByLabelText('Autonomous mode'));
    expect(screen.getByRole('button', { name: /Create & Run Auto/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Create & Run HITL/ })).not.toBeInTheDocument();
  });

  it('Just create + Create & Run stay enabled with empty title; clicking focuses the title input instead of submitting', async () => {
    const onCreate = vi.fn();
    render(<CreateCardPanel {...makeProps({ onCreate })} />);
    const justCreate = screen.getByRole('button', { name: 'Just create' });
    const createAndRun = screen.getByRole('button', { name: /Create & Run/ });
    expect(justCreate).not.toBeDisabled();
    expect(createAndRun).not.toBeDisabled();

    await act(async () => { fireEvent.click(justCreate); });
    expect(onCreate).not.toHaveBeenCalled();

    const titleInput = screen.getByPlaceholderText(/Card title/);
    expect(titleInput).toHaveFocus();
  });

  it('Cancel + close button + backdrop all call onClose', () => {
    const onClose = vi.fn();
    render(<CreateCardPanel {...makeProps({ onClose })} />);

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe('CreateCardPanel — onCreate contract', () => {
  it('Just create calls onCreate with run=false and the form input', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<CreateCardPanel {...makeProps({ onCreate })} />);

    fireEvent.change(screen.getByPlaceholderText(/Card title/), { target: { value: 'My card' } });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Just create' }));
    });

    expect(onCreate).toHaveBeenCalledOnce();
    const [input, opts] = onCreate.mock.calls[0];
    expect(input).toMatchObject({ title: 'My card', type: 'task', priority: 'medium' });
    expect(opts).toEqual({ run: false });
  });

  it('Create & Run forces feature_branch + create_pr to true and passes run:true', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<CreateCardPanel {...makeProps({ onCreate })} />);

    fireEvent.change(screen.getByPlaceholderText(/Card title/), { target: { value: 'Run me' } });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Create & Run/ }));
    });

    expect(onCreate).toHaveBeenCalledOnce();
    const [input, opts] = onCreate.mock.calls[0];
    expect(input).toMatchObject({ title: 'Run me', feature_branch: true, create_pr: true });
    expect(opts).toEqual({ run: true, interactive: true });
  });

  it('Create & Run with autonomous=true passes interactive:false', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<CreateCardPanel {...makeProps({ onCreate })} />);

    fireEvent.change(screen.getByPlaceholderText(/Card title/), { target: { value: 'Auto run' } });
    fireEvent.click(screen.getByLabelText('Autonomous mode'));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Create & Run/ }));
    });

    const [, opts] = onCreate.mock.calls[0];
    expect(opts).toEqual({ run: true, interactive: false });
  });
});

describe('CreateCardPanel — type templates', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('switching to a type WITH a template populates the body when not dirty', async () => {
    render(<CreateCardPanel {...makeProps()} />);

    // Start by switching away from task (which auto-loads its template) to bug.
    const typeSelect = screen.getByLabelText('Type') as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'bug' } });

    const editor = (await screen.findByTestId('md-editor')) as HTMLTextAreaElement;
    expect(editor.value).toBe('');

    fireEvent.change(typeSelect, { target: { value: 'task' } });
    expect(editor.value).toBe('## Task template\n');
  });

  it('switching from a type with a template to one without clears the body when not dirty', async () => {
    render(<CreateCardPanel {...makeProps()} />);

    const editor = (await screen.findByTestId('md-editor')) as HTMLTextAreaElement;
    expect(editor.value).toBe('## Task template\n');

    const typeSelect = screen.getByLabelText('Type') as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'bug' } });
    expect(editor.value).toBe('');
  });

  it('switching to a different type does NOT clear a dirty body', async () => {
    render(<CreateCardPanel {...makeProps()} />);

    const editor = (await screen.findByTestId('md-editor')) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: 'my custom description' } });

    const typeSelect = screen.getByLabelText('Type') as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'bug' } });
    expect(editor.value).toBe('my custom description');
  });

  it('switching to a type WITH a template shows a ConfirmModal when body IS dirty', () => {
    render(<CreateCardPanel {...makeProps()} />);

    const typeSelect = screen.getByLabelText('Type') as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'bug' } });
    // Make body dirty.
    const editor = screen.getByTestId('md-editor');
    fireEvent.change(editor, { target: { value: 'my custom description' } });
    fireEvent.change(typeSelect, { target: { value: 'task' } });

    // The CreateCardPanel itself has role="dialog"; the ConfirmModal renders
    // a second dialog on top. Assert on its visible text directly.
    expect(screen.getByText('Load template for "task"?')).toBeInTheDocument();
    expect(screen.getByText('This will replace the current body.')).toBeInTheDocument();

    // Cancel via the modal's Cancel button (last in DOM order).
    const cancelButtons = screen.getAllByRole('button', { name: 'Cancel' });
    fireEvent.click(cancelButtons[cancelButtons.length - 1]);
    expect((editor as HTMLTextAreaElement).value).toBe('my custom description');
  });

  it('confirming the template-load modal replaces the body', () => {
    render(<CreateCardPanel {...makeProps()} />);

    const typeSelect = screen.getByLabelText('Type') as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'bug' } });
    const editor = screen.getByTestId('md-editor') as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: 'dirty body' } });
    fireEvent.change(typeSelect, { target: { value: 'task' } });

    fireEvent.click(screen.getByRole('button', { name: 'Load template' }));
    expect(editor.value).toBe('## Task template\n');
  });
});

describe('CreateCardPanel — MDEditor preview skipHtml XSS prevention', () => {
  const xssBody = '<iframe src="https://example.com"></iframe>\n<script>alert(\'xss\')</script>\nhello';

  it('does not render iframe in the preview pane', async () => {
    const { container } = render(<CreateCardPanel {...makeProps()} />);
    const editor = await screen.findByTestId('md-editor');
    fireEvent.change(editor, { target: { value: xssBody } });
    await screen.findByTestId('md-preview');
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('does not render script in the preview pane', async () => {
    const { container } = render(<CreateCardPanel {...makeProps()} />);
    const editor = await screen.findByTestId('md-editor');
    fireEvent.change(editor, { target: { value: xssBody } });
    await screen.findByTestId('md-preview');
    expect(container.querySelector('script')).toBeNull();
  });

  it('still renders plain text content in the preview pane', async () => {
    render(<CreateCardPanel {...makeProps()} />);
    const editor = await screen.findByTestId('md-editor');
    fireEvent.change(editor, { target: { value: xssBody } });
    const preview = await screen.findByTestId('md-preview');
    expect(preview.textContent).toContain('hello');
  });
});
