import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CostCell } from './CostCell';
import { api } from '../../api/client';
import type { BlueprintCost } from '../../types';

vi.mock('../../api/client', () => ({ api: { setChatCostCap: vi.fn() } }));

const WITH_CAP: BlueprintCost = { spent_usd: 2.14, run_spent_usd: 0, cap_usd: 2, run_cap_usd: 0.5, priced: true };
const NO_CAP: BlueprintCost = { ...WITH_CAP, cap_usd: 0 };

function open(cost: BlueprintCost) {
  render(<CostCell chatId="S1" cost={cost} />);
  const trigger = screen.getByRole('button', { name: /^Cost:/ });
  fireEvent.click(trigger);
  return trigger;
}

const amount = () => screen.getByLabelText('Cap in USD') as HTMLInputElement;
const save = () => screen.getByRole('button', { name: 'Save' });

describe('CostCell', () => {
  beforeEach(() => {
    vi.mocked(api.setChatCostCap).mockReset().mockResolvedValue({} as never);
  });

  it('is a button named for the spend and the cap', () => {
    const { unmount } = render(<CostCell chatId="S1" cost={WITH_CAP} />);
    expect(screen.getByRole('button', { name: 'Cost: $2.14 of $2.00 cap, change cap' })).toBeInTheDocument();
    unmount();
    render(<CostCell chatId="S1" cost={NO_CAP} />);
    expect(screen.getByRole('button', { name: 'Cost: $2.14, no cap, set a cap' })).toBeInTheDocument();
  });

  it('opens with the spend and cap, and focus inside', () => {
    open(WITH_CAP);
    const dialog = screen.getByRole('dialog', { name: 'Cost cap' });
    expect(dialog).toHaveTextContent('$2.14 spent');
    expect(dialog).toHaveTextContent('Cap $2.00');
    expect(amount()).toHaveFocus();
  });

  it('opens with no cap set, to set one', () => {
    open(NO_CAP);
    expect(screen.getByRole('dialog', { name: 'Cost cap' })).toHaveTextContent('No cap');
  });

  it('fills the amount from a quick raise on the cap, or on the spend with no cap', () => {
    const { unmount } = render(<CostCell chatId="S1" cost={WITH_CAP} />);
    fireEvent.click(screen.getByRole('button', { name: /^Cost:/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Raise by $5' }));
    expect(amount().value).toBe('7.00');
    unmount();
    open(NO_CAP);
    fireEvent.click(screen.getByRole('button', { name: 'Raise by $2' }));
    expect(amount().value).toBe('4.14');
  });

  it('refuses an amount at or below the spend', () => {
    open(WITH_CAP);
    fireEvent.change(amount(), { target: { value: '2.14' } });
    expect(save()).toBeDisabled();
    expect(screen.getByText(/above the \$2\.14 spent/)).toBeInTheDocument();
    fireEvent.change(amount(), { target: { value: '2.15' } });
    expect(save()).toBeEnabled();
  });

  it('saves the amount and closes, returning focus to the cell', async () => {
    const trigger = open(WITH_CAP);
    fireEvent.change(amount(), { target: { value: '4.5' } });
    fireEvent.click(save());
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(api.setChatCostCap).toHaveBeenCalledWith('S1', 4.5);
    expect(trigger).toHaveFocus();
  });

  it('saves 0 for no cap', async () => {
    open(WITH_CAP);
    fireEvent.click(screen.getByLabelText('No cap'));
    fireEvent.click(save());
    await waitFor(() => expect(api.setChatCostCap).toHaveBeenCalledWith('S1', 0));
  });

  it('stays open with the error when the save fails', async () => {
    vi.mocked(api.setChatCostCap).mockRejectedValue(new Error('architect unreachable'));
    open(WITH_CAP);
    fireEvent.change(amount(), { target: { value: '4' } });
    fireEvent.click(save());
    expect(await screen.findByRole('alert')).toHaveTextContent('architect unreachable');
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(save()).toBeEnabled();
  });

  it('closes on Escape and on Cancel without posting', () => {
    const trigger = open(WITH_CAP);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(api.setChatCostCap).not.toHaveBeenCalled();
  });
});
