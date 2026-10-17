import { describe, it, expect, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { PromptActions } from './PromptActions';
import type { ChatPrompt } from './prompts';

const gate: ChatPrompt = {
  id: 'gate:sign-off:v2',
  freeText: false,
  actions: [
    { label: 'Approve', reply: 'approve', feedback: false, recommended: false },
    { label: 'Reject', reply: 'reject', feedback: true, recommended: false },
  ],
};

describe('PromptActions', () => {
  it('sends the clicked reply and stays disabled once sent', async () => {
    const onReply = vi.fn().mockResolvedValue(undefined);
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    });

    expect(onReply).toHaveBeenCalledWith('approve');
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    expect(onReply).toHaveBeenCalledTimes(1);
  });

  it('marks the group busy and disables every action while the send is in flight', async () => {
    let resolve!: () => void;
    const onReply = vi.fn(() => new Promise<void>((r) => { resolve = r; }));
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));

    expect(screen.getByRole('group', { name: 'Reply options' })).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByRole('button', { name: 'Reject' })).toBeDisabled();
    await act(async () => resolve());
  });

  it('re-enables the actions and shows the error when the send fails', async () => {
    const onReply = vi.fn().mockRejectedValue(new Error('backend unreachable'));
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('backend unreachable');
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled();
  });

  it('returns focus to the clicked action when a plain reply fails', async () => {
    const onReply = vi.fn().mockRejectedValue(new Error('backend unreachable'));
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));

    // The hand-back runs in an effect after the commit that shows the alert.
    await waitFor(() => expect(screen.getByRole('button', { name: 'Approve' })).toHaveFocus());
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('returns focus to the feedback box when a feedback reply fails', async () => {
    const onReply = vi.fn().mockRejectedValue(new Error('backend unreachable'));
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
    const box = screen.getByRole('textbox', { name: /Reject/ });
    fireEvent.change(box, { target: { value: 'too vague' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    // jsdom keeps focus on a disabled control, so watch for the explicit hand-back.
    const refocus = vi.spyOn(box, 'focus');

    await waitFor(() => expect(refocus).toHaveBeenCalled());
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(box).toHaveFocus();
  });

  it('asks for feedback before sending a feedback action', () => {
    const onReply = vi.fn().mockResolvedValue(undefined);
    render(<PromptActions prompt={gate} status="open" onReply={onReply} />);

    fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
    const box = screen.getByRole('textbox', { name: /Reject/ });
    expect(box).toHaveFocus();
    expect(onReply).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();

    fireEvent.change(box, { target: { value: 'needs a cache layer' } });
    fireEvent.keyDown(box, { key: 'Enter', shiftKey: true });
    expect(onReply).not.toHaveBeenCalled();

    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onReply).toHaveBeenCalledWith('reject needs a cache layer');
  });

  it('cancels feedback on Escape and returns focus to the action', () => {
    render(<PromptActions prompt={gate} status="open" onReply={vi.fn()} />);
    const reject = screen.getByRole('button', { name: 'Reject' });

    fireEvent.click(reject);
    fireEvent.keyDown(screen.getByRole('textbox', { name: /Reject/ }), { key: 'Escape' });

    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(reject).toHaveFocus();
  });

  it('disables every action while paused', () => {
    const onReply = vi.fn();
    render(<PromptActions prompt={gate} status="paused" onReply={onReply} />);

    for (const b of screen.getAllByRole('button')) expect(b).toBeDisabled();
  });

  it('replaces the actions with a summary naming the chosen action', () => {
    render(<PromptActions prompt={gate} status="answered" answer="Approve" />);

    expect(screen.queryAllByRole('button')).toHaveLength(0);
    expect(screen.getByTestId('prompt-summary')).toHaveTextContent('Approve');
  });

  it.each([
    ['superseded', { status: 'superseded' as const }],
    ['answered by free text', { status: 'answered' as const, answer: null }],
  ])('exposes a %s prompt as text, not as disabled buttons', (_name, props) => {
    render(<PromptActions prompt={gate} {...props} />);

    expect(screen.queryAllByRole('button')).toHaveLength(0);
    const summary = screen.getByTestId('prompt-summary');
    expect(summary).toBeVisible();
    expect(summary).not.toHaveAttribute('aria-hidden');
  });

  describe('layouts', () => {
    const LONG = 'Use the existing ledger table and add a nullable column so nothing needs a backfill migration';
    const question: ChatPrompt = {
      id: 'Q1',
      kind: 'question',
      freeText: true,
      actions: [
        { label: LONG, reply: '1', feedback: false, recommended: true },
        { label: 'Create a new table', reply: '2', feedback: false, recommended: false },
      ],
    };

    it('stacks a question as full-text options and sends the reply on click', async () => {
      const onReply = vi.fn().mockResolvedValue(undefined);
      render(<PromptActions prompt={question} status="open" onReply={onReply} />);

      const group = screen.getByRole('group', { name: 'Reply options' });
      expect(group).toHaveAttribute('data-layout', 'stacked');
      const long = screen.getByRole('button', { name: new RegExp(LONG) });
      expect(long).toHaveClass('chat-prompt-option');
      await act(async () => {
        fireEvent.click(long);
      });
      expect(onReply).toHaveBeenCalledWith('1');
    });

    it.each([undefined, 'mystery'])('stacks a prompt of kind %s too', (kind) => {
      render(<PromptActions prompt={{ ...question, kind }} status="open" onReply={vi.fn()} />);
      expect(screen.getByRole('group', { name: 'Reply options' })).toHaveAttribute('data-layout', 'stacked');
    });

    it('keeps a gate as the compact row', () => {
      render(<PromptActions prompt={{ ...gate, kind: 'gate' }} status="open" onReply={vi.fn()} />);
      expect(screen.getByRole('group', { name: 'Reply options' })).toHaveAttribute('data-layout', 'compact');
      expect(screen.getByRole('button', { name: 'Approve' })).not.toHaveClass('chat-prompt-option');
    });

    it('shows the recommended chip only on the recommended action, in both layouts', () => {
      const { unmount } = render(<PromptActions prompt={question} status="open" onReply={vi.fn()} />);
      expect(screen.getAllByText('recommended')).toHaveLength(1);
      expect(screen.getByRole('button', { name: new RegExp(LONG) })).toHaveTextContent('recommended');
      unmount();

      const g: ChatPrompt = {
        ...gate,
        kind: 'gate',
        actions: [{ ...gate.actions[0], recommended: true }, gate.actions[1]],
      };
      render(<PromptActions prompt={g} status="open" onReply={vi.fn()} />);
      expect(screen.getByRole('button', { name: 'Approve, recommended' })).toHaveTextContent('recommended');
      expect(screen.getByRole('button', { name: 'Reject' })).not.toHaveTextContent('recommended');
    });
  });

  describe('closed prompts', () => {
    const question: ChatPrompt = {
      id: 'Q1',
      kind: 'question',
      freeText: true,
      actions: [
        { label: 'SQLite', reply: '1', feedback: false, recommended: true },
        { label: 'Postgres', reply: '2', feedback: false, recommended: false },
      ],
    };

    const items = () => within(screen.getByTestId('prompt-options')).getAllByRole('listitem');
    const marked = () => items().filter((li) => li.getAttribute('data-chosen') === 'true');

    it('lists the offered options and marks the one that answered', () => {
      render(<PromptActions prompt={question} status="answered" answer="Postgres" />);

      expect(items()).toHaveLength(2);
      expect(items()[0]).toHaveTextContent('SQLite');
      expect(marked()).toHaveLength(1);
      expect(marked()[0]).toHaveTextContent('Postgres, chosen');
      expect(screen.queryAllByRole('button')).toHaveLength(0);
      expect(screen.getByTestId('prompt-summary')).toHaveTextContent('Answered: Postgres');
    });

    it.each([
      ['answered by free text', { status: 'answered' as const, answer: null }, 'Answered below'],
      ['superseded', { status: 'superseded' as const }, 'No longer open'],
    ])('lists the options with none marked when %s', (_name, props, summary) => {
      render(<PromptActions prompt={question} {...props} />);

      expect(items()).toHaveLength(2);
      expect(marked()).toHaveLength(0);
      expect(screen.getByTestId('prompt-summary')).toHaveTextContent(summary);
    });

    it('shows only the summary for a closed gate', () => {
      render(<PromptActions prompt={{ ...gate, kind: 'gate' }} status="answered" answer="Approve" />);

      expect(screen.queryByTestId('prompt-options')).not.toBeInTheDocument();
      expect(screen.getByTestId('prompt-summary')).toHaveTextContent('Answered: Approve');
    });

    it('does not list options while the prompt is open', () => {
      render(<PromptActions prompt={question} status="open" onReply={vi.fn()} />);

      expect(screen.queryByTestId('prompt-options')).not.toBeInTheDocument();
      expect(screen.getAllByRole('button')).toHaveLength(2);
    });
  });
});
