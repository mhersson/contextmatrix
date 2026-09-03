import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useTheme } from '../../hooks/useTheme';
import { useToast } from '../../hooks/useToast';
import type { PlaybookSummary } from '../../types';
import { isFullyComplete } from './playbookUtils';
import { PlaybookRow } from './PlaybookRow';
import { PlaybooksBar } from './PlaybooksBar';
import { CreatePlaybookForm, PlaybooksEmptyHero, createButtonStyle } from './PlaybooksEmptyHero';

export function PlaybooksPage() {
  const [playbooks, setPlaybooks] = useState<PlaybookSummary[] | null>(null);
  // Ephemeral by design - no localStorage; folding state resets on reload.
  const [completedOpen, setCompletedOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const { subscribe, reconnectEpoch } = useSSEBus();
  const { boardsRepos = [] } = useTheme();
  const multiRepo = boardsRepos.length > 1;
  const [newRepo, setNewRepo] = useState('');
  const { showToast } = useToast();
  const navigate = useNavigate();

  const fetchAll = useCallback(() => {
    api.listPlaybooks().then(setPlaybooks).catch(() => showToast('Failed to load playbooks', 'error'));
  }, [showToast]);

  useEffect(() => { fetchAll(); }, [fetchAll, reconnectEpoch]);
  useEffect(() => subscribe('playbook.*', fetchAll), [subscribe, fetchAll]);
  // Progress and segments derive from live card states, so card events
  // must refresh the list too, not just playbook mutations.
  useEffect(() => subscribe('card.*', fetchAll), [subscribe, fetchAll]);

  const active = (playbooks ?? []).filter((p) => !isFullyComplete(p));
  const completed = (playbooks ?? []).filter(isFullyComplete);

  const handleCreate = async () => {
    if (submitting) return;
    const title = newTitle.trim();
    if (!title) return;
    setSubmitting(true);
    try {
      const detail = await api.createPlaybook(
        multiRepo ? { title, boards_repo: newRepo || boardsRepos[0].name } : { title },
      );
      navigate(`/playbooks/${detail.id}`);
    } catch {
      showToast('Failed to create playbook', 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const cancelCreate = () => {
    setCreating(false);
    setNewTitle('');
    setNewRepo('');
  };

  const isEmpty = playbooks !== null && active.length === 0 && completed.length === 0;

  if (isEmpty) {
    return (
      <div className="h-full flex flex-col">
        <PlaybooksBar />
        <PlaybooksEmptyHero
          creating={creating}
          onStartCreate={() => setCreating(true)}
          title={newTitle}
          onTitleChange={setNewTitle}
          onCreate={handleCreate}
          onCancel={cancelCreate}
          submitting={submitting}
          repos={boardsRepos}
          repo={newRepo || boardsRepos[0]?.name}
          onRepoChange={setNewRepo}
        />
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <PlaybooksBar />
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center justify-between mb-4">
          <div>
            <p className="font-mono text-xs" style={{ color: 'var(--grey0)' }}>playbooks</p>
            <h1
              style={{
                color: 'var(--fg)',
                fontFamily: 'var(--font-display)',
                fontWeight: 500,
                fontSize: '24px',
                letterSpacing: '-0.015em',
                lineHeight: 1.2,
              }}
            >
              Playbooks
            </h1>
          </div>
          {creating ? (
            <CreatePlaybookForm
              title={newTitle}
              onTitleChange={setNewTitle}
              onCreate={handleCreate}
              onCancel={cancelCreate}
              submitting={submitting}
              repos={boardsRepos}
              repo={newRepo || boardsRepos[0]?.name}
              onRepoChange={setNewRepo}
            />
          ) : (
            <button
              onClick={() => setCreating(true)}
              className="px-3 py-1.5 rounded text-sm font-medium"
              style={createButtonStyle}
            >
              New playbook
            </button>
          )}
        </div>

        {playbooks === null ? (
          <div style={{ color: 'var(--grey1)' }}>Loading...</div>
        ) : (
          <>
            {active.map((p) => <PlaybookRow key={p.id} playbook={p} />)}

            {completed.length > 0 && (
              <div className="mt-4">
                <button
                  onClick={() => setCompletedOpen((v) => !v)}
                  className="text-sm mb-2"
                  style={{ color: 'var(--grey1)' }}
                >
                  {completedOpen ? '▾' : '▸'} Completed ({completed.length})
                </button>
                {completedOpen && (
                  <div style={{ opacity: 0.6 }}>
                    {completed.map((p) => <PlaybookRow key={p.id} playbook={p} />)}
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
