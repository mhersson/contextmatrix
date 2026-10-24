import { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { api } from '../../api/client';
import { notifyChatSessionsChanged, useChatSessions } from '../../hooks/useChatSessions';
import { useSessionPin } from '../../hooks/useSessionPin';
import { useToast } from '../../hooks/useToast';
import { sessionKind } from '../../lib/sessions';
import type { SessionKind } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { PageBar } from '../SidebarMenuButton/PageBar';
import { EMPTY_FILTER, filterSessions, isEmptyFilter, newestActivityFirst, type SessionFilter } from './sessionFilter';
import { SessionsBulkBar } from './SessionsBulkBar';
import { SessionsEmpty } from './SessionsEmpty';
import { SESSIONS_PANEL_ID, SessionsTabs } from './SessionsTabs';
import { SessionsTable } from './SessionsTable';
import { SessionsToolbar } from './SessionsToolbar';

const TAB_PARAM: Record<SessionKind, string> = { chat: 'chats', architect: 'blueprints' };

const plural = (n: number) => `${n} session${n === 1 ? '' : 's'}`;

export function SessionsPage() {
  const [params, setParams] = useSearchParams();
  const tab: SessionKind = params.get('tab') === TAB_PARAM.architect ? 'architect' : 'chat';
  const navigate = useNavigate();
  const { sessions, error, loaded } = useChatSessions();
  const pin = useSessionPin();
  const { showToast } = useToast();
  const [filter, setFilter] = useState<SessionFilter>(EMPTY_FILTER);
  const [picked, setPicked] = useState<ReadonlySet<string>>(() => new Set());
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  const tabSessions = sessions.filter((s) => sessionKind(s) === tab);
  const rows = newestActivityFirst(filterSessions(tabSessions, filter));
  // Bulk actions only ever touch ticked rows the user can see.
  const selected = rows.filter((s) => picked.has(s.id)).map((s) => s.id);
  // A tick on a row that stops showing (tab, filters or a refresh) is dropped, never kept hidden.
  const visibleKey = rows.map((s) => s.id).join(' ');
  const [prunedFor, setPrunedFor] = useState(visibleKey);
  if (prunedFor !== visibleKey) {
    setPrunedFor(visibleKey);
    setPicked(new Set(selected));
  }
  const counts: Record<SessionKind, number> = {
    chat: sessions.filter((s) => sessionKind(s) === 'chat').length,
    architect: sessions.filter((s) => sessionKind(s) === 'architect').length,
  };

  const switchTab = (next: SessionKind) => {
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      p.set('tab', TAB_PARAM[next]);
      return p;
    }, { replace: true });
  };

  const toggle = (id: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const toggleAll = () =>
    setPicked(selected.length === rows.length ? new Set() : new Set(rows.map((s) => s.id)));

  const bulk = async (action: () => Promise<void>) => {
    setBusy(true);
    try {
      await action();
    } finally {
      setBusy(false);
    }
  };

  const deleteSelected = () =>
    bulk(async () => {
      setConfirming(false);
      const ids = selected;
      const results = await Promise.allSettled(ids.map((id) => api.deleteChat(id)));
      const failed = ids.filter((_, i) => results[i].status === 'rejected');
      setPicked(new Set(failed));
      notifyChatSessionsChanged();
      if (failed.length > 0) showToast(`Could not delete ${plural(failed.length)}`, 'error');
    });

  return (
    <div className="h-full overflow-y-auto">
      <PageBar title="Sessions" />
      <div className="ssn-page">
        <h1 className="ssn-title">Sessions</h1>
        <SessionsTabs active={tab} counts={counts} onChange={switchTab} />
        <div id={SESSIONS_PANEL_ID} role="tabpanel" aria-labelledby={`sessions-tab-${tab}`} aria-busy={!loaded}>
          <SessionsToolbar sessions={tabSessions} filter={filter} onChange={setFilter} />
          {error && <p className="ssn-error" role="alert">Could not load sessions: {error}</p>}
          {selected.length > 0 && (
            <SessionsBulkBar
              count={selected.length}
              busy={busy}
              onPin={() => void bulk(() => pin(selected, true))}
              onUnpin={() => void bulk(() => pin(selected, false))}
              onDelete={() => setConfirming(true)}
            />
          )}
          {!loaded ? (
            <p className="ssn-loading">Loading…</p>
          ) : rows.length > 0 ? (
            <SessionsTable
              rows={rows}
              showMode={tab === 'architect'}
              selected={picked}
              onToggle={toggle}
              onToggleAll={toggleAll}
              onPin={(s) => void pin([s.id], !s.pinned)}
              onOpen={(id) => navigate(`/chat/${id}`)}
            />
          ) : (
            !error && (
              <SessionsEmpty
                kind={tab}
                filtered={tabSessions.length > 0 && !isEmptyFilter(filter)}
                onClear={() => setFilter(EMPTY_FILTER)}
              />
            )
          )}
        </div>
      </div>
      <ConfirmModal
        open={confirming}
        title={`Delete ${plural(selected.length)}?`}
        message="This removes each session and its transcript permanently."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={() => void deleteSelected()}
        onCancel={() => setConfirming(false)}
      />
    </div>
  );
}
