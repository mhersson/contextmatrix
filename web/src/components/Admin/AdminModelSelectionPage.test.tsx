import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { AdminModelSelectionPage } from './AdminModelSelectionPage';
import type { ModelOutcomeStats, ModelOutcomeEntry, ModelBlacklistEntry } from '../../types';

const mocks = vi.hoisted(() => ({
  adminModelOutcomes: vi.fn(),
  adminResetModelOutcomes: vi.fn(),
  adminModelBlacklist: vi.fn(),
  adminDelistModel: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminModelOutcomes: mocks.adminModelOutcomes,
      adminResetModelOutcomes: mocks.adminResetModelOutcomes,
      adminModelBlacklist: mocks.adminModelBlacklist,
      adminDelistModel: mocks.adminDelistModel,
    },
  };
});

function entry(overrides: Partial<ModelOutcomeEntry> = {}): ModelOutcomeEntry {
  return {
    model: 'deepseek/deepseek-v4-flash',
    race_samples: 8,
    race_wins: 5,
    race_win_rate: 0.625,
    solo_samples: 14,
    solo_failures: 2,
    total_cost_usd: 1.42,
    ...overrides,
  };
}

function stats(overrides: Partial<ModelOutcomeStats> = {}): ModelOutcomeStats {
  return {
    total_samples: 84,
    models: [entry()],
    ...overrides,
  };
}

function blacklistEntry(overrides: Partial<ModelBlacklistEntry> = {}): ModelBlacklistEntry {
  return {
    slug: 'moonshotai/kimi-k3',
    reason: 'tool calls failed to parse on 3 consecutive turns',
    sample_card: 'CM-101',
    reported_by: 'agent:worker-1',
    first_seen: 1756400000,
    last_seen: 1756500000,
    ...overrides,
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.adminModelBlacklist.mockResolvedValue({ models: [] });
});

describe('AdminModelSelectionPage - list', () => {
  it('renders a row per model with race and solo stats kept separate', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(
      stats({
        models: [
          entry({
            model: 'deepseek/deepseek-v4-flash',
            race_samples: 8,
            race_wins: 5,
            race_win_rate: 0.625,
            solo_samples: 14,
            solo_failures: 2,
            total_cost_usd: 1.42,
          }),
          entry({
            model: 'qwen/qwen3-max',
            race_samples: 0,
            race_wins: 0,
            race_win_rate: 0,
            solo_samples: 6,
            solo_failures: 1,
            total_cost_usd: 0.31,
          }),
        ],
      }),
    );

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument());
    expect(screen.getByText('qwen/qwen3-max')).toBeInTheDocument();
    expect(screen.getByText('63%')).toBeInTheDocument();
    expect(screen.getByText('14')).toBeInTheDocument();
    expect(screen.getByText('$1.42')).toBeInTheDocument();
    // A model that never raced shows no race win rate at all - a solo
    // completion is not a win over anything.
    expect(screen.queryByText('0%')).not.toBeInTheDocument();
  });

  it('shows the total recorded outcome count', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats({ total_samples: 84 }));

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText(/84 total recorded outcomes/)).toBeInTheDocument());
  });

  it('falls back to a generic message when adminModelOutcomes rejects with a non-APIError shape', async () => {
    mocks.adminModelOutcomes.mockRejectedValue({ error: 12345 });

    render(<AdminModelSelectionPage />);

    expect(await screen.findByText('Failed to load model outcomes.')).toBeInTheDocument();
    expect(screen.queryByText('12345')).not.toBeInTheDocument();
  });

  it('shows an empty-state message when no outcomes are recorded', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats({ total_samples: 0, models: [] }));

    render(<AdminModelSelectionPage />);

    expect(await screen.findByText(/no model outcomes recorded/i)).toBeInTheDocument();
  });
});

describe('AdminModelSelectionPage - reset flow', () => {
  it('opens a confirm dialog stating the total row count, then resets and refetches on confirm', async () => {
    mocks.adminModelOutcomes
      .mockResolvedValueOnce(stats({ total_samples: 84, models: [entry()] }))
      .mockResolvedValueOnce(stats({ total_samples: 0, models: [] }));
    mocks.adminResetModelOutcomes.mockResolvedValue({ deleted: 84 });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'Reset selection data' }));

    const dialog = await screen.findByRole('dialog');
    expect(
      within(dialog).getByText('Delete all 84 recorded outcomes? This clears the observability ledger; model selection is unaffected.'),
    ).toBeInTheDocument();
    expect(mocks.adminResetModelOutcomes).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /reset/i }));

    await waitFor(() => expect(mocks.adminResetModelOutcomes).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(mocks.adminModelOutcomes).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText('deepseek/deepseek-v4-flash')).not.toBeInTheDocument());
    expect(screen.getByText(/no model outcomes recorded/i)).toBeInTheDocument();
  });

  it('cancelling the confirm dialog does not reset', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats({ total_samples: 84, models: [entry()] }));

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Reset selection data' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /cancel/i }));

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(mocks.adminResetModelOutcomes).not.toHaveBeenCalled();
    expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument();
  });

  it('surfaces a reset failure as an inline error without crashing', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats({ total_samples: 84, models: [entry()] }));
    mocks.adminResetModelOutcomes.mockRejectedValue({ code: 'INTERNAL_ERROR', error: 'failed to reset model outcomes' });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Reset selection data' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /reset/i }));

    await waitFor(() => expect(mocks.adminResetModelOutcomes).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/failed to reset model outcomes/i)).toBeInTheDocument();

    // Component survives the error - the row is still rendered, not crashed.
    expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument();
  });
});

describe('AdminModelSelectionPage - blacklist', () => {
  it('renders a row per blacklisted model with slug, reason, sample card, and reporter', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats());
    mocks.adminModelBlacklist.mockResolvedValue({
      models: [
        blacklistEntry(),
        blacklistEntry({ slug: 'x-ai/grok-5-mini', reason: 'no forward progress', sample_card: '', reported_by: 'agent:worker-2' }),
      ],
    });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    expect(screen.getByText('x-ai/grok-5-mini')).toBeInTheDocument();
    expect(screen.getByText('tool calls failed to parse on 3 consecutive turns')).toBeInTheDocument();
    expect(screen.getByText('CM-101')).toBeInTheDocument();
    expect(screen.getByText('agent:worker-1')).toBeInTheDocument();
  });

  it('shows an empty-state message when nothing is blacklisted', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats());

    render(<AdminModelSelectionPage />);

    expect(await screen.findByText(/no models are blacklisted/i)).toBeInTheDocument();
  });

  it('delist opens a confirm dialog, then deletes the slug and refetches on confirm', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats());
    mocks.adminModelBlacklist
      .mockResolvedValueOnce({ models: [blacklistEntry()] })
      .mockResolvedValueOnce({ models: [] });
    mocks.adminDelistModel.mockResolvedValue({ deleted: 'moonshotai/kimi-k3' });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /delist/i }));

    await waitFor(() => expect(mocks.adminDelistModel).toHaveBeenCalledWith('moonshotai/kimi-k3'));
    await waitFor(() => expect(mocks.adminModelBlacklist).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText('moonshotai/kimi-k3')).not.toBeInTheDocument());
  });

  it('cancelling the delist dialog does not delete', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats());
    mocks.adminModelBlacklist.mockResolvedValue({ models: [blacklistEntry()] });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /cancel/i }));

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();
  });

  it('surfaces a delist failure as an inline error without crashing', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats());
    mocks.adminModelBlacklist.mockResolvedValue({ models: [blacklistEntry()] });
    mocks.adminDelistModel.mockRejectedValue({ code: 'INTERNAL_ERROR', error: 'failed to delete blacklist entry' });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /delist/i }));

    await waitFor(() => expect(mocks.adminDelistModel).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/failed to delete blacklist entry/i)).toBeInTheDocument();
    expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument();
  });
});
