import { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { api } from '../../api/client';
import { notifyChatSessionsChanged, useChatSessions } from '../../hooks/useChatSessions';
import { useAttention } from '../../hooks/useAttention';
import { usePlaybooks } from '../../hooks/usePlaybooks';
import { useSessionPin } from '../../hooks/useSessionPin';
import { SESSIONS_TAB, sessionKind, sessionsTab, type SessionsTab } from '../../lib/sessions';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { EMPTY_PLAYBOOK_FILTER, type PlaybookFilter } from '../Playbooks/playbookFilter';
import { PlaybooksTab } from '../Playbooks/PlaybooksTab';
import { PageBar } from '../SidebarMenuButton/PageBar';
import { EMPTY_FILTER, filterSessions, isEmptyFilter, newestActivityFirst, type SessionFilter } from './sessionFilter';
import { SessionsBulkBar } from './SessionsBulkBar';
import { SessionsEmpty } from './SessionsEmpty';
import { SESSIONS_PANEL_ID, SessionsTabs } from './SessionsTabs';
import { SessionsTable } from './SessionsTable';
import { SessionsToolbar } from './SessionsToolbar';
import { useBulkActions } from './useBulkActions';
import { useRowSelection } from './useRowSelection';

const plural = (n: number) => `${n} session${n === 1 ? '' : 's'}`;

export function SessionsPage() {
  const [params, setParams] = useSearchParams();
  const tab = sessionsTab(params.get('tab'));
  const navigate = useNavigate();
  const { sessions, error, loaded } = useChatSessions();
  const { playbooks, loaded: playbooksLoaded, error: playbooksError } = usePlaybooks();
  const pin = useSessionPin();
  const { blueprints, playbooks: waitingPlaybooks } = useAttention();
  const [filter, setFilter] = useState<SessionFilter>(EMPTY_FILTER);
  const [playbookFilter, setPlaybookFilter] = useState<PlaybookFilter>(EMPTY_PLAYBOOK_FILTER);
  const [confirming, setConfirming] = useState(false);

  const tabSessions = sessions.filter((s) => sessionKind(s) === tab);
  // Status waiting exists on Blueprints only; the tab comes from the URL, so derive rather than reset.
  const sessionFilter: SessionFilter =
    tab !== 'architect' && filter.status === 'waiting' ? { ...filter, status: 'any' } : filter;
  const rows = newestActivityFirst(filterSessions(tabSessions, sessionFilter, new Set(blueprints.keys())));
  const { picked, selected, toggle, toggleAll, tickOnly } = useRowSelection(rows.map((s) => s.id));
  const { busy, run, deleteTicked } = useBulkActions({ selected, tickOnly });
  const counts: Record<SessionsTab, number> = {
    chat: sessions.filter((s) => sessionKind(s) === 'chat').length,
    architect: sessions.filter((s) => sessionKind(s) === 'architect').length,
    playbooks: playbooks.length,
  };

  const waitingTabs = new Set<SessionsTab>();
  if (blueprints.size > 0) waitingTabs.add('architect');
  if (waitingPlaybooks.size > 0) waitingTabs.add('playbooks');

  const switchTab = (next: SessionsTab) => {
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      p.set('tab', SESSIONS_TAB[next]);
      return p;
    }, { replace: true });
  };

  return (
    <div className="h-full overflow-y-auto">
      <PageBar title="Sessions" />
      <div className="ssn-page">
        <h1 className="ssn-title">Sessions</h1>
        <SessionsTabs active={tab} counts={counts} onChange={switchTab} waiting={waitingTabs} />
        <div
          id={SESSIONS_PANEL_ID}
          role="tabpanel"
          aria-labelledby={`sessions-tab-${tab}`}
          aria-busy={tab === 'playbooks' ? !playbooksLoaded && !playbooksError : !loaded}
        >
          {tab === 'playbooks' ? (
            <PlaybooksTab filter={playbookFilter} onFilterChange={setPlaybookFilter} />
          ) : (
            <>
              <SessionsToolbar sessions={tabSessions} filter={sessionFilter} onChange={setFilter} offerWaiting={tab === 'architect'} />
              {error && <p className="ssn-error" role="alert">Could not load sessions: {error}</p>}
              {selected.length > 0 && (
                <SessionsBulkBar
                  label="Selected sessions"
                  count={selected.length}
                  busy={busy}
                  onPin={() => void run(() => pin(selected, true))}
                  onUnpin={() => void run(() => pin(selected, false))}
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
                  waiting={blueprints}
                />
              ) : (
                !error && (
                  <SessionsEmpty
                    emptyText={tab === 'architect' ? 'No blueprints yet.' : 'No chats yet.'}
                    filtered={tabSessions.length > 0 && !isEmptyFilter(sessionFilter)}
                    onClear={() => setFilter(EMPTY_FILTER)}
                  />
                )
              )}
            </>
          )}
        </div>
      </div>
      <ConfirmModal
        open={confirming}
        title={`Delete ${plural(selected.length)}?`}
        message="This removes each session and its transcript permanently."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={() => {
          setConfirming(false);
          void deleteTicked((id) => api.deleteChat(id), notifyChatSessionsChanged, plural);
        }}
        onCancel={() => setConfirming(false)}
      />
    </div>
  );
}
