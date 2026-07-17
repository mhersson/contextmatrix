import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { AdminModelSelectionPage } from './AdminModelSelectionPage';
import type { ModelOutcomeStats, ModelOutcomeEntry } from '../../types';

const mocks = vi.hoisted(() => ({
  adminModelOutcomes: vi.fn(),
  adminResetModelOutcomes: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminModelOutcomes: mocks.adminModelOutcomes,
      adminResetModelOutcomes: mocks.adminResetModelOutcomes,
    },
  };
});

function entry(overrides: Partial<ModelOutcomeEntry> = {}): ModelOutcomeEntry {
  return {
    model: 'deepseek/deepseek-v4-flash',
    samples: 22,
    wins: 13,
    win_rate: 0.59,
    expected_wins: 9.5,
    total_cost_usd: 1.42,
    active: true,
    ...overrides,
  };
}

function stats(overrides: Partial<ModelOutcomeStats> = {}): ModelOutcomeStats {
  return {
    outcome_floor: 20,
    total_samples: 84,
    models: [entry()],
    ...overrides,
  };
}

beforeEach(() => {
  vi.resetAllMocks();
});

describe('AdminModelSelectionPage - list', () => {
  it('renders a row per model with samples, wins, win rate, cost, and active/inert label', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(
      stats({
        models: [
          entry({ model: 'deepseek/deepseek-v4-flash', samples: 22, wins: 13, win_rate: 0.59, total_cost_usd: 1.42, active: true }),
          entry({ model: 'qwen/qwen3-max', samples: 5, wins: 1, win_rate: 0.2, total_cost_usd: 0.31, active: false }),
        ],
      }),
    );

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('deepseek/deepseek-v4-flash')).toBeInTheDocument());
    expect(screen.getByText('qwen/qwen3-max')).toBeInTheDocument();
    expect(screen.getByText('22')).toBeInTheDocument();
    expect(screen.getByText('13')).toBeInTheDocument();
    expect(screen.getByText('59%')).toBeInTheDocument();
    expect(screen.getByText('20%')).toBeInTheDocument();
    expect(screen.getByText('$1.42')).toBeInTheDocument();
    expect(screen.getByText('Active')).toBeInTheDocument();
    expect(screen.getByText('Inert')).toBeInTheDocument();
  });

  it('shows total sample count and the configured outcome floor', async () => {
    mocks.adminModelOutcomes.mockResolvedValue(stats({ outcome_floor: 20, total_samples: 84 }));

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText(/Outcome floor: 20 samples/)).toBeInTheDocument());
    expect(screen.getByText(/84 total recorded outcomes/)).toBeInTheDocument();
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
      within(dialog).getByText('Delete all 84 recorded outcomes? Model selection returns to priors-only.'),
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
