import { useState } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import { errorMessage } from '../../lib/errors';
import type { ModelBlacklist, ModelOutcomeStats } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ModelBlacklistTable } from './ModelBlacklistTable';
import { ModelOutcomesTable } from './ModelOutcomesTable';

const EMPTY_STATS: ModelOutcomeStats = { total_samples: 0, models: [] };
const EMPTY_BLACKLIST: ModelBlacklist = { models: [] };

const fetchOutcomes = () => api.adminModelOutcomes();
const fetchBlacklist = () => api.adminModelBlacklist();

/** Admin-only Model selection data page: the recorded-outcome ledger (race
 * and solo stats kept separate; observability only, selection never reads
 * it) plus a destructive reset that wipes every recorded outcome, and the
 * incapable-model blacklist with per-row delisting. Open in none mode (see
 * AdminGuard), admin-gated in multi mode - same trust posture as project
 * management. Owns all data fetching and the mutations; the tables it
 * renders are purely presentational. */
export function AdminModelSelectionPage() {
  const {
    items: stats,
    loading,
    listError,
    actionError,
    setActionError,
    refetch,
  } = useAdminResource(fetchOutcomes, EMPTY_STATS, 'Failed to load model outcomes.');

  const blacklist = useAdminResource(fetchBlacklist, EMPTY_BLACKLIST, 'Failed to load model blacklist.');

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [delistSlug, setDelistSlug] = useState<string | null>(null);

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

  const confirmDelist = async () => {
    const slug = delistSlug;
    setDelistSlug(null);
    if (!slug) return;
    await blacklist.act(() => api.adminDelistModel(slug), 'Failed to delist model.');
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
        {`${stats.total_samples} total recorded outcomes · observability only, selection is priors-based`}
      </p>

      {actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {actionError}
        </div>
      )}

      <ModelOutcomesTable models={stats.models} loading={loading} error={listError} />

      <h2 className="text-base font-semibold mt-2" style={{ color: 'var(--fg)' }}>
        Blacklisted models
      </h2>

      <p className="text-sm" style={{ color: 'var(--grey1)' }}>
        Models the agent backend reported incapable. Blacklisted models are excluded from every
        automatic pick; only a card pin overrides. Delisting makes a model selectable again.
      </p>

      {blacklist.actionError && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {blacklist.actionError}
        </div>
      )}

      <ModelBlacklistTable
        models={blacklist.items.models}
        loading={blacklist.loading}
        error={blacklist.listError}
        onDelist={setDelistSlug}
      />

      <ConfirmModal
        open={confirmOpen}
        title="Reset selection data?"
        message={`Delete all ${stats.total_samples} recorded outcomes? This clears the observability ledger; model selection is unaffected.`}
        variant="danger"
        confirmLabel="Reset"
        onConfirm={() => void confirmReset()}
        onCancel={() => setConfirmOpen(false)}
      />

      <ConfirmModal
        open={delistSlug !== null}
        title="Delist model?"
        message={`Remove ${delistSlug ?? ''} from the blacklist? It becomes selectable for automatic picks again.`}
        confirmLabel="Delist"
        onConfirm={() => void confirmDelist()}
        onCancel={() => setDelistSlug(null)}
      />
    </div>
  );
}
