import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { GateBar } from './GateBar';
import type { ChatPrompt } from '../ChatPanel/prompts';
import { LEDGER_REVIEW_FAILED, LEDGER_REVIEW_V2 } from '../../test-utils/blueprintFixtures';

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
    render(<GateBar stage="solution_design" version={2} prompt={prompt} status="open" review={null} onReply={onReply} />);
    expect(screen.getByRole('region', { name: 'Gate decision' })).toHaveTextContent('Design rev 2 is waiting for your decision');
    fireEvent.click(screen.getByRole('button', { name: /^Approve/ }));
    await waitFor(() => expect(onReply).toHaveBeenCalledWith('approve'));
  });

  it('asks for feedback before a reject', async () => {
    const onReply = vi.fn().mockResolvedValue(undefined);
    render(<GateBar stage="solution_design" version={2} prompt={prompt} status="open" review={null} onReply={onReply} />);
    fireEvent.click(screen.getByRole('button', { name: 'Reject slicing' }));
    fireEvent.change(screen.getByLabelText('Feedback for Reject slicing'), { target: { value: 'merge them' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    await waitFor(() => expect(onReply).toHaveBeenCalledWith('reject slicing merge them'));
  });

  it('names a critical finding not yet fixed at the gate', () => {
    render(
      <GateBar
        stage="solution_design"
        version={3}
        prompt={prompt}
        status="open"
        review={{ review: LEDGER_REVIEW_FAILED, version: 3 }}
        onReply={vi.fn()}
      />,
    );
    const bar = screen.getByRole('region', { name: 'Gate decision' });
    expect(bar).toHaveTextContent('R5 critical, open: The encoder change also indents the error bodies other routes share.');
    // R4 is still open but minor: only the ledger behind the toggle lists it.
    expect(bar).not.toHaveTextContent('The README update');
  });

  it('keeps the whole ledger behind a collapsed toggle', () => {
    render(
      <GateBar
        stage="solution_design"
        version={3}
        prompt={prompt}
        status="open"
        review={{ review: LEDGER_REVIEW_FAILED, version: 3 }}
        onReply={vi.fn()}
      />,
    );
    const toggle = screen.getByRole('button', { name: 'Findings (3)' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('list', { name: 'Findings' })).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const list = screen.getByRole('list', { name: 'Findings' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(3);
    expect(document.getElementById(toggle.getAttribute('aria-controls') ?? '')).toContainElement(list);
  });

  it('names nothing when every critical finding is fixed', () => {
    // A round that fixed everything it judged passes and sends its version to the gate.
    const fixed = { ...LEDGER_REVIEW_V2, findings: LEDGER_REVIEW_V2.findings.slice(0, 2) };
    render(
      <GateBar
        stage="solution_design"
        version={2}
        prompt={prompt}
        status="open"
        review={{ review: fixed, version: 2 }}
        onReply={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Findings (2)' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Gate decision' })).not.toHaveTextContent('critical');
  });

  it('shows no ledger without a review of the version at the gate', () => {
    render(<GateBar stage="solution_design" version={2} prompt={prompt} status="open" review={null} onReply={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /^Findings/ })).not.toBeInTheDocument();
  });
});
