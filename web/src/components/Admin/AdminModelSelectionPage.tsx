import { useMemo, useState } from 'react';
import type { CSSProperties } from 'react';
import { api } from '../../api/client';
import { useAdminResource } from '../../hooks/useAdminResource';
import { errorMessage } from '../../lib/errors';
import type { ModelBlacklist, SelectorCandidatesResponse, SelectorLadders, SelectorLaddersResponse, SelectorTier, TierBars } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { LadderPanel } from './LadderPanel';
import { ModelBlacklistTable } from './ModelBlacklistTable';
import { ModelContextMenu } from './ModelContextMenu';
import { PickPreview } from './PickPreview';
import { SourceStrip } from './SourceStrip';
import { ROLES, isMonotone, laddersEqual, rolesEqual } from './ladder';
import { useSelectorPreview } from './useSelectorPreview';

// Placeholders behind the loading state; never rendered as data. The
// built-in ladder here matches selection.DefaultTierBars only so a
// half-loaded page has a valid shape.
const BUILTIN_BARS: TierBars = { simple: 0.5, moderate: 0.76, complex: 0.82, critical: 0.9 };
const EMPTY_LADDERS: SelectorLaddersResponse = {
  ladders: { coder: { ...BUILTIN_BARS }, reviewer: { ...BUILTIN_BARS } },
  defaults: { ...BUILTIN_BARS },
  headroom: 1.5,
  headroom_default: 1.5,
  is_default: true,
};
const EMPTY_CATALOG: SelectorCandidatesResponse = {
  candidates: [],
  favorites: [],
  blacklist: [],
  quality_floor: 0.5,
  catalog_refreshed_at: '',
  reasoning_effort: '',
  intelligence_index_version: '',
  aa_scored_models: 0,
  served_scored_models: 0,
  best_aa_score: 0,
};
const EMPTY_BLACKLIST: ModelBlacklist = { models: [] };

const fetchLadders = () => api.adminSelectorLadders();
const fetchCandidates = () => api.adminSelectorCandidates();
const fetchBlacklist = () => api.adminModelBlacklist();

/** What the operator is editing: an override over the saved response. */
interface SelectorDraft {
  ladders: SelectorLadders;
  headroom: number;
}

/** The model a right-click landed on and where. */
interface ModelMenu {
  slug: string;
  x: number;
  y: number;
}

/** Admin-only tier ladders page: edits the per-role selector ladders CM
 * stores and sends with every run, previews picks through the shared
 * selector, and hosts the incapable-model blacklist. Open in none mode (see
 * AdminGuard), admin-gated in multi mode. The draft is an override over the
 * saved response, so a refetch can never clobber an edit and discarding is
 * dropping the override. */
export function AdminModelSelectionPage() {
  const saved = useAdminResource(fetchLadders, EMPTY_LADDERS, 'Failed to load the tier ladders.');
  const catalog = useAdminResource(fetchCandidates, EMPTY_CATALOG, 'Failed to load the candidate catalog.');
  const blacklist = useAdminResource(fetchBlacklist, EMPTY_BLACKLIST, 'Failed to load model blacklist.');

  const [draft, setDraft] = useState<SelectorDraft | null>(null);
  // Starts on whatever the saved ladders are, and Discard and Reset turn it
  // back on: off shows one set of handles, which must never hide a reviewer
  // ladder that differs from it.
  const [separate, setSeparate] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [delistSlug, setDelistSlug] = useState<string | null>(null);
  const [menu, setMenu] = useState<ModelMenu | null>(null);
  const [selectedTier, setSelectedTier] = useState<SelectorTier>('complex');

  const ladders = draft?.ladders ?? saved.items.ladders;
  const headroom = draft?.headroom ?? saved.items.headroom;
  // NaN (an emptied field) compares unequal, so an emptied field is dirty
  // and can be discarded, while Save stays disabled through headroomValid.
  const dirty = draft !== null && (!laddersEqual(draft.ladders, saved.items.ladders) || !(Math.abs(draft.headroom - saved.items.headroom) < 1e-9));
  const monotone = ROLES.every((r) => isMonotone(ladders[r]));
  const headroomValid = Number.isFinite(headroom) && headroom >= 1;
  const catalogReady = !catalog.loading && catalog.listError === null;
  // Without a loaded ladder the bars on screen are the placeholder, not what
  // the next run uses: nothing may claim them as saved, and nothing may edit
  // or save them back.
  const laddersReady = !saved.loading && saved.listError === null;
  const editable = catalogReady && laddersReady;

  const setLadders = (next: SelectorLadders) => setDraft((d) => ({ ladders: next, headroom: d?.headroom ?? saved.items.headroom }));
  const setHeadroom = (next: number) => setDraft((d) => ({ ladders: d?.ladders ?? saved.items.ladders, headroom: next }));

  // The catalog's blacklist is what strikes the dots and what re-keys the
  // preview after an add or a delist.
  const preview = useSelectorPreview(ladders, headroom, editable && headroomValid, catalog.items.blacklist);

  const blacklisted = useMemo(() => new Set(catalog.items.blacklist), [catalog.items.blacklist]);

  const changeSeparate = (next: boolean) => {
    if (!next && !rolesEqual(ladders)) setLadders({ coder: { ...ladders.coder }, reviewer: { ...ladders.coder } });
    setSeparate(next);
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setSaveError(null);
    try {
      await api.adminSelectorPutLadders(draft.ladders, draft.headroom);
      await saved.refetch();
      setDraft(null);
    } catch (err) {
      setSaveError(errorMessage(err, 'Failed to save the selection settings.'));
    } finally {
      setSaving(false);
    }
  };

  const discard = () => {
    setDraft(null);
    setSaveError(null);
    setSeparate(true);
  };

  const reset = () => {
    setDraft({ ladders: { coder: { ...saved.items.defaults }, reviewer: { ...saved.items.defaults } }, headroom: saved.items.headroom_default });
    setSeparate(true);
  };

  // Every blacklist change refetches the table and refreshes the catalog:
  // the catalog carries the blacklist the chart and the preview read. A
  // refresh that fails keeps the loaded catalog (and the draft) on screen.
  const mutateBlacklist = async (fn: () => Promise<unknown>, failMessage: string) => {
    await blacklist.act(fn, failMessage);
    await catalog.refresh('Failed to refresh the candidate catalog.');
  };

  const confirmDelist = async () => {
    const slug = delistSlug;
    setDelistSlug(null);
    if (!slug) return;
    await mutateBlacklist(() => api.adminDelistModel(slug), 'Failed to delist model.');
  };

  const addToBlacklist = (slug: string) => void mutateBlacklist(() => api.adminBlacklistModel(slug), 'Failed to blacklist model.');

  const openModelMenu = (slug: string, x: number, y: number) => setMenu({ slug, x, y });

  // The loading panel stands in for the chart whenever either half is missing;
  // the ladders error wins because it blocks editing outright.
  const panelError = saved.listError ?? catalog.listError;
  const panelLoading = saved.loading ? 'Loading the tier ladders…' : 'Loading the candidate catalog…';

  const statusText = saved.loading
    ? 'loading the ladders…'
    : saved.listError
      ? 'ladders unavailable'
      : dirty
        ? 'unsaved changes · next run still uses the saved values'
        : 'saved · in effect for the next run';

  return (
    <div className="apd-root tl-page">
      <header className="apd-strip">
        <div className="min-w-0 shrink-0">
          <p className="apd-strip-eyebrow">
            Admin <span aria-hidden="true">·</span> Model selection
          </p>
          <h1 className="apd-strip-title">Tier ladders</h1>
        </div>
        <p className="apd-strip-summary tl-summary">
          Every candidate by price and quality, one line per tier bar. Drag a handle; the picks, the panel and the price band follow.
          Nothing is sent until you save.
        </p>
        <div className="apd-strip-actions">
          <span className={`tl-status${dirty ? ' dirty' : ''}`} data-testid="tl-status" aria-live="polite">
            <span className="tl-status-dot" aria-hidden="true" />
            {statusText}
          </span>
          <button type="button" className="bf-btn-ghost" onClick={reset} disabled={!laddersReady}>
            Reset to defaults
          </button>
          <button type="button" className="bf-btn-ghost" onClick={discard} disabled={!dirty || !laddersReady}>
            Discard changes
          </button>
          <button
            type="button"
            className="bf-btn-primary"
            onClick={() => void save()}
            disabled={!dirty || !monotone || !headroomValid || saving || !laddersReady}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </header>

      <div className="tl-body">
        <SourceStrip catalog={catalog.items} ready={catalogReady} loading={catalog.loading} />
        {saveError && (
          <div className="tl-error" role="alert">
            {saveError}
          </div>
        )}
        {blacklist.actionError && (
          <div className="tl-error" role="alert">
            {blacklist.actionError}
          </div>
        )}
        {catalog.actionError && (
          <div className="tl-error" role="alert">
            {catalog.actionError}
          </div>
        )}

        <div className="tl-grid">
          {editable ? (
            <LadderPanel
              candidates={catalog.items.candidates}
              ladders={ladders}
              separate={separate}
              onSeparateChange={changeSeparate}
              onChange={setLadders}
              headroom={headroom}
              onHeadroomChange={setHeadroom}
              floor={catalog.items.quality_floor}
              bestAAScore={catalog.items.best_aa_score}
              indexVersion={catalog.items.intelligence_index_version}
              selectedTier={selectedTier}
              tierPreview={preview.preview?.tiers[selectedTier] ?? null}
              appliedHeadroom={preview.appliedHeadroom}
              blacklist={blacklisted}
              onModelMenu={openModelMenu}
            />
          ) : (
            <section className="apd-panel" style={{ '--apd-acc': 'var(--aqua)' } as CSSProperties}>
              <div className="apd-panel-head">
                <h2 className="apd-panel-title">Tier ladder</h2>
              </div>
              <div className={`apd-panel-empty${panelError ? ' tl-error' : ''}`} role={panelError ? 'alert' : undefined}>
                {panelError ?? panelLoading}
              </div>
            </section>
          )}
          <PickPreview
            preview={preview.preview}
            pending={preview.pending}
            disabled={!editable}
            error={preview.error}
            ladders={ladders}
            separate={separate}
            candidates={catalog.items.candidates}
            blacklist={blacklisted}
            selectedTier={selectedTier}
            onSelectTier={setSelectedTier}
            headroom={headroomValid ? headroom : (preview.appliedHeadroom ?? NaN)}
            onModelMenu={openModelMenu}
          />
        </div>

        <ModelBlacklistTable models={blacklist.items.models} loading={blacklist.loading} error={blacklist.listError} onDelist={setDelistSlug} />
      </div>

      {menu && (
        <ModelContextMenu
          slug={menu.slug}
          x={menu.x}
          y={menu.y}
          blacklisted={blacklisted.has(menu.slug)}
          onBlacklist={addToBlacklist}
          onDelist={setDelistSlug}
          onClose={() => setMenu(null)}
        />
      )}

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
