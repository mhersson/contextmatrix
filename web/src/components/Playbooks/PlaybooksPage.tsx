import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useToast } from '../../hooks/useToast';
import type { PlaybookSummary } from '../../types';
import { isFullyComplete } from './playbookUtils';
import { PlaybookRow } from './PlaybookRow';
import { PlaybooksBar } from './PlaybooksBar';

const buttonStyle = {
  backgroundColor: 'var(--bg-green)',
  color: 'var(--green)',
  border: '1px solid var(--green)',
};

export function PlaybooksPage() {
  const [playbooks, setPlaybooks] = useState<PlaybookSummary[] | null>(null);
  // Ephemeral by design - no localStorage; folding state resets on reload.
  const [completedOpen, setCompletedOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const { subscribe, reconnectEpoch } = useSSEBus();
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
    const title = newTitle.trim();
    if (!title) return;
    try {
      const detail = await api.createPlaybook({ title });
      navigate(`/playbooks/${detail.id}`);
    } catch {
      showToast('Failed to create playbook', 'error');
    }
  };

  const cancelCreate = () => {
    setCreating(false);
    setNewTitle('');
  };

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
            <div className="flex items-center gap-2">
              <input
                autoFocus
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleCreate();
                  if (e.key === 'Escape') cancelCreate();
                }}
                placeholder="Playbook title"
                className="px-3 py-1.5 rounded text-sm"
                style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
              />
              <button onClick={handleCreate} className="px-3 py-1.5 rounded text-sm font-medium" style={buttonStyle}>
                Create
              </button>
              <button onClick={cancelCreate} className="px-3 py-1.5 rounded text-sm" style={{ color: 'var(--grey1)' }}>
                Cancel
              </button>
            </div>
          ) : (
            <button
              onClick={() => setCreating(true)}
              className="px-3 py-1.5 rounded text-sm font-medium"
              style={buttonStyle}
            >
              New playbook
            </button>
          )}
        </div>

        {playbooks === null ? (
          <div style={{ color: 'var(--grey1)' }}>Loading...</div>
        ) : (
          <>
            {active.length === 0 && completed.length === 0 && (
              <div style={{ color: 'var(--grey1)' }}>No playbooks yet.</div>
            )}
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
