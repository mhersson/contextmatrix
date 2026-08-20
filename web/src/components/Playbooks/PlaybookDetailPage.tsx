import { useCallback, useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router';
import type { DragEndEvent } from '@dnd-kit/core';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useToast } from '../../hooks/useToast';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import type { NewPlaybookEntry, PlaybookDetail, PlaybookSegment } from '../../types';
import { arrayMoveLocal, persistReorder } from './playbookUtils';
import { PlaybookDetailHeader } from './PlaybookDetailHeader';
import { PlaybookEntryList } from './PlaybookEntryList';
import { AddEntryComposer } from './AddEntryComposer';

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
    promise.then(setDetail).catch(() => { showToast('Update failed', 'error'); fetchDetail(); });
  }, [showToast, fetchDetail]);

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
    applyPatch(api.addPlaybookEntry(detail.id, entry));
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

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-6 max-w-3xl mx-auto">
        <PlaybookDetailHeader
          detail={detail}
          segments={entrySegments(detail)}
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

        <AddEntryComposer onAdd={handleAdd} />
      </div>

      <ConfirmModal
        open={deleteOpen}
        title={`Delete playbook ${detail.id}?`}
        message="This removes the playbook. Its history is preserved in git."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={handleDelete}
        onCancel={() => setDeleteOpen(false)}
      />
    </div>
  );
}
