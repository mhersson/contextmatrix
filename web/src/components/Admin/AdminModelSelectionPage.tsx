import { useState } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import { errorMessage } from '../../lib/errors';
import type { ModelOutcomeStats } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ModelOutcomesTable } from './ModelOutcomesTable';

const EMPTY_STATS: ModelOutcomeStats = { outcome_floor: 0, total_samples: 0, models: [] };

const fetchOutcomes = () => api.adminModelOutcomes();

/** Admin-only Model selection data page: per-model Best-of-N outcome stats
 * (samples, wins, win rate, cost, active-vs-floor status) plus a destructive
 * reset that wipes every recorded outcome. Open in none mode (see
 * AdminGuard), admin-gated in multi mode - same trust posture as project
 * management. Owns all data fetching and the reset mutation; the table it
 * renders is purely presentational. */
export function AdminModelSelectionPage() {
  const {
    items: stats,
    loading,
    listError,
    actionError,
    setActionError,
    refetch,
  } = useAdminResource(fetchOutcomes, EMPTY_STATS, 'Failed to load model outcomes.');

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [resetting, setResetting] = useState(false);

  // Bespoke rather than act() - the reset owns a busy flag for the button.
  const confirmReset = async () => {
    setConfirmOpen(false);
    setActionError(null);
    setResetting(true);
    try {
      await api.adminResetModelOutcomes();
      await refetch();
    } catch (err) {
      setActionError(errorMessage(err, 'Failed to reset model outcomes.'));
    } finally {
      setResetting(false);
    }
  };

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
          Model selection data
        </h1>
        <button
          type="button"
          onClick={() => setConfirmOpen(true)}
          disabled={resetting}
          className="rounded py-1.5 px-4 font-medium disabled:opacity-60"
          style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
        >
          {resetting ? 'Resetting…' : 'Reset selection data'}
        </button>
      </div>

      <p className="text-sm" style={{ color: 'var(--grey1)' }}>
        {`Outcome floor: ${stats.outcome_floor} samples · ${stats.total_samples} total recorded outcomes`}
      </p>

      {actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {actionError}
        </div>
      )}

      <ModelOutcomesTable models={stats.models} loading={loading} error={listError} />

      <ConfirmModal
        open={confirmOpen}
        title="Reset selection data?"
        message={`Delete all ${stats.total_samples} recorded outcomes? Model selection returns to priors-only.`}
        variant="danger"
        confirmLabel="Reset"
        onConfirm={() => void confirmReset()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}
