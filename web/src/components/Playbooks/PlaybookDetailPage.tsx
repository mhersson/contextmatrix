import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router';
import type { DragEndEvent } from '@dnd-kit/core';
import { api, isAPIError } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useToast } from '../../hooks/useToast';
import { usePlaybookBranches } from '../../hooks/usePlaybookBranches';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import type { NewPlaybookEntry, PlaybookDetail, PlaybookSegment } from '../../types';
import { arrayMoveLocal, isRunActive, persistReorder } from './playbookUtils';
import { PlaybookDetailHeader } from './PlaybookDetailHeader';
import { PlaybookEntryList } from './PlaybookEntryList';
import { PlaybookSidePanel } from './PlaybookSidePanel';
import { PlaybooksBar } from './PlaybooksBar';

// Progress segments derive from live card state, not a stored field, so they
// stay in sync with card.* events that only trigger a refetch.
function entrySegments(detail: PlaybookDetail): PlaybookSegment[] {
  return detail.entries.map((e) =>
    e.complete ? 'complete' : e.card_state === 'in_progress' ? 'active' : e.missing ? 'missing' : 'pending',
  );
}

export function PlaybookDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { subscribe, reconnectEpoch } = useSSEBus();
  const { showToast } = useToast();

  const [detail, setDetail] = useState<PlaybookDetail | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editingTitle, setEditingTitle] = useState(false);
  const [editingDescription, setEditingDescription] = useState(false);
  const [runnableConfirmOpen, setRunnableConfirmOpen] = useState(false);
  const [stopConfirmOpen, setStopConfirmOpen] = useState(false);

  const playbookProjects = useMemo(
    () => [...new Set((detail?.entries ?? []).flatMap((e) => (e.type === 'card' && e.project ? [e.project] : [])))],
    [detail],
  );
  const { branches, loading: branchesLoading, error: branchesError } = usePlaybookBranches(playbookProjects, !!detail);

  const fetchDetail = useCallback(() => {
    api.getPlaybook(id)
      .then((d) => { setDetail(d); setNotFound(false); })
      .catch(() => setNotFound(true));
  }, [id]);

  useEffect(() => { fetchDetail(); }, [fetchDetail, reconnectEpoch]);
  useEffect(
    () => subscribe('playbook.*', (e) => { if (e.data?.id === id) fetchDetail(); }),
    [subscribe, fetchDetail, id],
  );
  // Progress and per-entry chips derive from live card state, so any card
  // event touching one of this playbook's projects must refresh too.
  useEffect(
    () => subscribe('card.*', (e) => { if (detail?.entries.some((en) => en.project === e.project)) fetchDetail(); }),
    [subscribe, fetchDetail, detail],
  );

  const applyPatch = useCallback((promise: Promise<PlaybookDetail>) => {
    return promise.then(setDetail).catch(() => { showToast('Update failed', 'error'); fetchDetail(); });
  }, [showToast, fetchDetail]);

  const describeApiError = (err: unknown, fallback: string) =>
    isAPIError(err) ? (err.details ? `${err.error}: ${err.details}` : err.error) : fallback;

  const patchRunnable = useCallback(async (runnable: boolean) => {
    if (!detail) return;
    try {
      setDetail(await api.patchPlaybook(detail.id, { runnable }));
    } catch (err) {
      showToast(describeApiError(err, runnable ? 'Could not make the playbook runnable' : 'Update failed'), 'error');
      fetchDetail();
    }
  }, [detail, showToast, fetchDetail]);

  const handleToggleRunnable = useCallback((next: boolean) => {
    if (next) setRunnableConfirmOpen(true);
    else void patchRunnable(false);
  }, [patchRunnable]);

  const handleSaveBaseBranch = useCallback((value: string) => {
    if (!detail) return;
    applyPatch(api.patchPlaybook(detail.id, { base_branch: value }));
  }, [detail, applyPatch]);

  const handleRun = useCallback(async () => {
    if (!detail) return;
    try {
      setDetail(await api.runPlaybook(detail.id));
    } catch (err) {
      showToast(describeApiError(err, 'Could not start the playbook'), 'error');
      fetchDetail();
    }
  }, [detail, showToast, fetchDetail]);

  const handleStopConfirm = useCallback(async () => {
    setStopConfirmOpen(false);
    if (!detail) return;
    try {
      setDetail(await api.stopPlaybook(detail.id));
    } catch (err) {
      showToast(describeApiError(err, 'Could not stop the playbook'), 'error');
      fetchDetail();
    }
  }, [detail, showToast, fetchDetail]);

  const handleToggleDone = useCallback((entryId: string, done: boolean) => {
    if (!detail) return;
    applyPatch(api.patchPlaybookEntry(detail.id, entryId, { done }));
  }, [detail, applyPatch]);

  const handleSaveNote = useCallback((entryId: string, note: string) => {
    if (!detail) return;
    applyPatch(api.patchPlaybookEntry(detail.id, entryId, { note }));
  }, [detail, applyPatch]);

  const handleSaveText = useCallback((entryId: string, text: string) => {
    if (!detail) return;
    applyPatch(api.patchPlaybookEntry(detail.id, entryId, { text }));
  }, [detail, applyPatch]);

  const handleRemove = useCallback((entryId: string) => {
    if (!detail) return;
    applyPatch(api.deletePlaybookEntry(detail.id, entryId));
  }, [detail, applyPatch]);

  const handleAdd = useCallback(async (entry: NewPlaybookEntry) => {
    if (!detail) return;
    await applyPatch(api.addPlaybookEntry(detail.id, entry));
  }, [detail, applyPatch]);

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || !detail) return;
    const activeId = String(active.id);
    const overId = String(over.id);
    const from = detail.entries.findIndex((e) => e.id === activeId);
    const to = detail.entries.findIndex((e) => e.id === overId);
    if (activeId === overId || from < 0 || to < 0) return;

    const snapshot = detail;
    setDetail({ ...detail, entries: arrayMoveLocal(detail.entries, from, to) });
    persistReorder(snapshot.id, snapshot, activeId, overId)
      .then((updated) => { if (updated) setDetail(updated); })
      .catch(() => { showToast('Reorder failed', 'error'); fetchDetail(); });
  }, [detail, showToast, fetchDetail]);

  const saveTitle = (value: string) => {
    setEditingTitle(false);
    const title = value.trim();
    if (!detail || !title || title === detail.title) return;
    applyPatch(api.patchPlaybook(detail.id, { title }));
  };

  const saveDescription = (value: string) => {
    setEditingDescription(false);
    if (!detail) return;
    applyPatch(api.patchPlaybook(detail.id, { description: value.trim() }));
  };

  const handleDelete = async () => {
    if (!detail) return;
    try {
      await api.deletePlaybook(detail.id);
      navigate('/playbooks');
    } catch {
      showToast('Failed to delete playbook', 'error');
    }
    setDeleteOpen(false);
  };

  if (notFound) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-2" style={{ color: 'var(--grey1)' }}>
        <p>Playbook not found.</p>
        <Link to="/playbooks" style={{ color: 'var(--aqua)' }}>Back to playbooks</Link>
      </div>
    );
  }

  if (!detail) {
    return <div className="p-6" style={{ color: 'var(--grey1)' }}>Loading...</div>;
  }

  const runnableCards = detail.entries.filter((e) => e.type === 'card' && !e.complete && !e.missing);
  const runnableProjects = [...new Set(runnableCards.map((e) => e.project ?? ''))];
  const runnableMessage = (
    <>
      <p className="mb-2">Making this playbook runnable will:</p>
      <ul className="list-disc pl-5 flex flex-col gap-1">
        <li>Set <strong>autonomous</strong>, <strong>create PR</strong>, <strong>wait for CI</strong> and <strong>merge PR</strong> on {runnableCards.length} card{runnableCards.length === 1 ? '' : 's'}, with base branch <code>playbook/{detail.id}</code>.</li>
        <li>Create the branch <code>playbook/{detail.id}</code> from {detail.base_branch ? <code>{detail.base_branch}</code> : 'the repository default'} in: {runnableProjects.join(', ') || 'no projects yet'}. The first card that runs in each repository creates it.</li>
        <li>Lock those five settings on the cards and disable their run buttons while the playbook runs.</li>
        <li>Run every card autonomously; no human-in-the-loop.</li>
        <li>Unchecking later does not revert the card settings.</li>
      </ul>
    </>
  );

  const deleteMessage = isRunActive(detail.run)
    ? 'This removes the playbook. Its run is active: the current card keeps running as an ordinary card. History is preserved in git.'
    : 'This removes the playbook. Its history is preserved in git.';

  return (
    <div className="h-full overflow-y-auto">
      <PlaybooksBar />
      <div className="p-6 pb-page">
        <div className="pb-workbench">
          <div className="pb-track">
            <PlaybookDetailHeader
              detail={detail}
              editingTitle={editingTitle}
              editingDescription={editingDescription}
              onStartEditTitle={() => setEditingTitle(true)}
              onStartEditDescription={() => setEditingDescription(true)}
              onSaveTitle={saveTitle}
              onSaveDescription={saveDescription}
              onDeleteClick={() => setDeleteOpen(true)}
            />

            <PlaybookEntryList
              entries={detail.entries}
              onDragEnd={handleDragEnd}
              onToggleDone={handleToggleDone}
              onSaveNote={handleSaveNote}
              onSaveText={handleSaveText}
              onRemove={handleRemove}
            />
          </div>

          <PlaybookSidePanel
            detail={detail}
            segments={entrySegments(detail)}
            onAdd={handleAdd}
            branches={branches}
            branchesLoading={branchesLoading}
            branchesError={branchesError}
            onToggleRunnable={handleToggleRunnable}
            onSaveBaseBranch={handleSaveBaseBranch}
            onRun={() => void handleRun()}
            onStop={() => setStopConfirmOpen(true)}
          />
        </div>
      </div>

      <ConfirmModal
        open={deleteOpen}
        title={`Delete playbook ${detail.id}?`}
        message={deleteMessage}
        variant="danger"
        confirmLabel="Delete"
        onConfirm={handleDelete}
        onCancel={() => setDeleteOpen(false)}
      />
      <ConfirmModal
        open={runnableConfirmOpen}
        title={`Make ${detail.title} runnable?`}
        message={runnableMessage}
        confirmLabel="Make runnable"
        onConfirm={() => { setRunnableConfirmOpen(false); void patchRunnable(true); }}
        onCancel={() => setRunnableConfirmOpen(false)}
      />
      <ConfirmModal
        open={stopConfirmOpen}
        title="Stop the playbook run?"
        message="The current card's worker will be killed. Uncommitted work in that container is lost. Play resumes from the current entry."
        variant="danger"
        confirmLabel="Stop run"
        onConfirm={() => void handleStopConfirm()}
        onCancel={() => setStopConfirmOpen(false)}
      />
    </div>
  );
}
