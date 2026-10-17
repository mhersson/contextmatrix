import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { GateBar } from './GateBar';
import type { ChatPrompt } from '../ChatPanel/prompts';

const prompt: ChatPrompt = {
  id: 'gate:solution_design:v2',
  kind: 'gate',
  freeText: false,
  actions: [
    { label: 'Approve', reply: 'approve', feedback: false, recommended: true },
    { label: 'Reject with feedback', reply: 'reject', feedback: true, recommended: false },
    { label: 'Reject slicing', reply: 'reject slicing', feedback: true, recommended: false },
  ],
};

describe('GateBar', () => {
  it('names the gate and sends a reply through the given path', async () => {
    const onReply = vi.fn().mockResolvedValue(undefined);
    render(<GateBar stage="solution_design" version={2} prompt={prompt} onReply={onReply} />);
    expect(screen.getByRole('region', { name: 'Gate decision' })).toHaveTextContent('Design rev 2 is waiting for your decision');
    fireEvent.click(screen.getByRole('button', { name: /^Approve/ }));
    await waitFor(() => expect(onReply).toHaveBeenCalledWith('approve'));
  });

  it('asks for feedback before a reject', async () => {
    const onReply = vi.fn().mockResolvedValue(undefined);
    render(<GateBar stage="solution_design" version={2} prompt={prompt} onReply={onReply} />);
    fireEvent.click(screen.getByRole('button', { name: 'Reject slicing' }));
    fireEvent.change(screen.getByLabelText('Feedback for Reject slicing'), { target: { value: 'merge them' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    await waitFor(() => expect(onReply).toHaveBeenCalledWith('reject slicing merge them'));
  });
});
