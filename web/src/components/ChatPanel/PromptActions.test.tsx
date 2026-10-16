import { describe, it, expect, vi } from 'vitest';
import { act, render, screen, fireEvent } from '@testing-library/react';
import { PromptActions } from './PromptActions';
import type { ChatPrompt } from './prompts';

const gate: ChatPrompt = {
  id: 'gate:sign-off:v2',
  freeText: false,
  actions: [
    { label: 'Approve', reply: 'approve', feedback: false },
    { label: 'Reject', reply: 'reject', feedback: true },
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

    await screen.findByRole('alert');
    expect(screen.getByRole('button', { name: 'Approve' })).toHaveFocus();
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

    await screen.findByRole('alert');
    expect(refocus).toHaveBeenCalled();
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
});
