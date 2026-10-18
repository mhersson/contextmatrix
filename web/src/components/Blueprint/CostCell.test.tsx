import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CostCell } from './CostCell';
import { api } from '../../api/client';
import { CHAT_SESSIONS_CHANGED_EVENT } from '../../hooks/useChatSessions';
import { resetChatModelsCache } from '../../utils/chatModels';
import type { BlueprintCost } from '../../types';

vi.mock('../../api/client', () => ({
  api: { setChatCostCap: vi.fn(), listChats: vi.fn(), listChatModels: vi.fn() },
}));

const WITH_CAP: BlueprintCost = { spent_usd: 2.14, run_spent_usd: 0, cap_usd: 2, run_cap_usd: 0.5, priced: true };
const ROOMY_CAP: BlueprintCost = { ...WITH_CAP, cap_usd: 5 };
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
    vi.mocked(api.listChats).mockReset().mockResolvedValue([{ id: 'S1', model: 'arch/priced' }] as never);
    vi.mocked(api.listChatModels).mockReset().mockResolvedValue({
      source: 'endpoint',
      default: '',
      architect_default: 'arch/default',
      models: [
        { id: 'arch/priced', label: 'priced', max_tokens: 1 },
        { id: 'arch/free', label: 'free', max_tokens: 1, unpriced: true },
        { id: 'arch/default', label: 'default', max_tokens: 1, unpriced: true },
      ],
    } as never);
    resetChatModelsCache();
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

  it('announces the change on success and not on failure', async () => {
    const changed = vi.fn();
    window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, changed);
    try {
      vi.mocked(api.setChatCostCap).mockRejectedValueOnce(new Error('nope'));
      open(WITH_CAP);
      fireEvent.change(amount(), { target: { value: '4' } });
      fireEvent.click(save());
      await screen.findByRole('alert');
      expect(changed).not.toHaveBeenCalled();
      fireEvent.click(save());
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(changed).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, changed);
    }
  });

  it('refuses a positive cap on a model with no price but allows No cap', async () => {
    vi.mocked(api.listChats).mockResolvedValue([{ id: 'S1', model: 'arch/free' }] as never);
    open(ROOMY_CAP);
    await waitFor(() => expect(screen.getByText(/No price is known for this model/)).toBeInTheDocument());
    expect(save()).toBeDisabled();
    fireEvent.click(screen.getByLabelText('No cap'));
    expect(save()).toBeEnabled();
  });

  it('prices a session with no model of its own by the architect default', async () => {
    vi.mocked(api.listChats).mockResolvedValue([{ id: 'S1', model: '' }] as never);
    open(ROOMY_CAP);
    await waitFor(() => expect(screen.getByText(/No price is known for this model/)).toBeInTheDocument());
    expect(save()).toBeDisabled();
  });

  it('does not offer Save for No cap when there is no cap to remove', () => {
    open(NO_CAP);
    fireEvent.click(screen.getByLabelText('No cap'));
    expect(save()).toBeDisabled();
  });
});
