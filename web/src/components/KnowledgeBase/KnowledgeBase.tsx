import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api, errorMessage } from '../../api/client';
import { KnowledgeBaseSidebar } from './KnowledgeBaseSidebar';
import { KnowledgeDocViewer } from './KnowledgeDocViewer';
import { useUnsavedGuard } from './useUnsavedGuard';
import { useKnowledgeBaseData } from './useKnowledgeBaseData';
import { useKnowledgeRefreshStatus } from './useKnowledgeRefreshStatus';
import { RefreshPlanModal } from './RefreshPlanModal';
import type { RefreshJobStatus, RefreshPlan } from '../../types';

export function KnowledgeBase({ project }: { project: string }) {
  const navigate = useNavigate();
  const params = useParams();
  const splat = params['*'] ?? '';
  const [splatRepo, splatDoc] = splat.split('/');
  // Memoize so the reload useCallback (which depends on selected) keeps a
  // stable identity across renders when the URL doesn't change.
  const selected = useMemo(
    () =>
      splatRepo && splatDoc
        ? { repo: decodeURIComponent(splatRepo), doc: decodeURIComponent(splatDoc) }
        : null,
    [splatRepo, splatDoc],
  );

  const handleSelect = (sel: { repo: string; doc: string }) => {
    navigate(
      `/projects/${project}/knowledge/${encodeURIComponent(sel.repo)}/${encodeURIComponent(sel.doc)}`,
    );
  };

  const { setDirty, guard, modal } = useUnsavedGuard<{ repo: string; doc: string }>(handleSelect);

  const {
    summary,
    summaryError,
    loading,
    docContent,
    docLoading,
    docError,
    setDocContent,
    setDocError,
    setSummary,
    setSummaryError,
  } = useKnowledgeBaseData(project, selected);

  const refreshStatus = useKnowledgeRefreshStatus(project);
  const [planModal, setPlanModal] = useState<{ repo: string; plan: RefreshPlan } | null>(null);

  const reload = useCallback(async () => {
    if (selected) {
      try {
        const refreshed = await api.getKnowledgeDoc(project, selected.repo, selected.doc);
        setDocContent(refreshed);
        setDocError(null);
      } catch (err) {
        setDocError(errorMessage(err));
      }
    }
    try {
      const fullSummary = await api.getKnowledgeBase(project);
      setSummary(fullSummary);
      setSummaryError(null);
    } catch (err) {
      setSummaryError(errorMessage(err));
    }
  }, [project, selected, setDocContent, setDocError, setSummary, setSummaryError]);

  const handleRefreshClick = async (repo: string) => {
    try {
      const plan = await api.getKnowledgeRefreshPlan(project, repo);
      setPlanModal({ repo, plan });
    } catch (err) {
      console.error('failed to fetch refresh plan', err);
    }
  };

  const handleConfirmRefresh = async (overwriteDocs: string[]) => {
    if (!planModal) return;
    try {
      await api.startKnowledgeRefresh(project, planModal.repo, overwriteDocs);
      refreshStatus.refresh();
    } catch (err) {
      console.error('failed to start refresh', err);
    } finally {
      setPlanModal(null);
    }
  };

  // After a successful refresh, reload the summary so docs reflect new content.
  // refreshStatus.repos identity changes per poll tick, so we track the prior
  // snapshot and only reload on the rising edge into 'succeeded' for any repo.
  // Without this, multi-repo refreshes (one running, one already succeeded)
  // would refetch the summary every poll interval for the duration of the
  // running repo's job.
  const prevReposRef = useRef<Record<string, RefreshJobStatus>>({});
  // Reset the snapshot when switching projects so a stale 'succeeded' state
  // from the previous project does not suppress the rising-edge detector
  // for the new one.
  useEffect(() => {
    prevReposRef.current = {};
  }, [project]);
  useEffect(() => {
    const prev = prevReposRef.current;
    const curr = refreshStatus.repos;
    const newlySucceeded = Object.entries(curr).some(
      ([repo, job]) => job.state === 'succeeded' && prev[repo]?.state !== 'succeeded',
    );
    prevReposRef.current = curr;
    if (newlySucceeded) {
      void reload();
    }
  }, [refreshStatus.repos, reload]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full" style={{ color: 'var(--grey1)' }}>
        <div className="text-sm">Loading knowledge base...</div>
      </div>
    );
  }

  // Full-page error only when we have nothing to show (initial load failed).
  // When summary already exists, render the error inline near the sidebar
  // below so the user keeps their selection and the rest of the UI.
  if (summaryError && !summary) {
    return (
      <div className="p-6">
        <p className="text-sm" style={{ color: 'var(--red)' }}>
          Failed to load knowledge base: {summaryError}
        </p>
      </div>
    );
  }

  if (!summary || summary.repos.length === 0) {
    return (
      <div className="p-6" style={{ color: 'var(--grey1)' }}>
        <p>No repos configured for this project.</p>
        <p className="mt-2 text-sm">
          Add a <code style={{ color: 'var(--fg)' }}>repos:</code> entry to the project's{' '}
          <code style={{ color: 'var(--fg)' }}>.board.yaml</code> to enable knowledge base
          generation.
        </p>
      </div>
    );
  }

  return (
    <div className="flex h-full">
      <div className="flex flex-col h-full min-h-0">
        {summaryError && (
          <div
            className="px-3 py-2 text-xs"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
            role="alert"
          >
            Failed to refresh knowledge base: {summaryError}
          </div>
        )}
        <KnowledgeBaseSidebar
          summary={summary}
          selected={selected}
          onSelect={guard}
          onRefreshClick={handleRefreshClick}
          refreshStatusByRepo={refreshStatus.repos}
        />
      </div>
      <div className="flex-1 min-w-0 min-h-0 overflow-hidden">
        {docError ? (
          <div className="p-6">
            <p className="text-sm" style={{ color: 'var(--red)' }}>
              Failed to load doc: {docError}
            </p>
          </div>
        ) : docLoading ? (
          <div className="p-6" style={{ color: 'var(--grey1)' }}>
            Loading…
          </div>
        ) : docContent && selected ? (
          <KnowledgeDocViewer
            key={`${selected.repo}/${selected.doc}`}
            project={project}
            repo={selected.repo}
            doc={selected.doc}
            response={docContent}
            repoSummary={summary?.repos.find((r) => r.name === selected.repo)}
            onSaved={reload}
            onDirtyChange={setDirty}
            onRefreshClick={handleRefreshClick}
            refreshing={
              refreshStatus.repos[selected.repo]?.state === 'planning' ||
              refreshStatus.repos[selected.repo]?.state === 'running'
            }
          />
        ) : summary.repos.every((r) => r.docs.length === 0) ? (
          <div className="p-6" style={{ color: 'var(--grey1)' }}>
            <p>No knowledge base docs yet for this project.</p>
            <p className="mt-2 text-sm">
              Click the Refresh button next to a repo on the left to build the KB. Alternatively
              run{' '}
              <code
                className="px-1 rounded"
                style={{ backgroundColor: 'var(--bg1)', color: 'var(--fg)' }}
              >
                /contextmatrix:refresh-knowledge --project {project}
              </code>{' '}
              in your Claude Code session.
            </p>
          </div>
        ) : (
          <div className="p-6" style={{ color: 'var(--grey1)' }}>
            Select a doc.
          </div>
        )}
      </div>
      {modal}
      {planModal && (
        <RefreshPlanModal
          plan={planModal.plan}
          repo={planModal.repo}
          onConfirm={handleConfirmRefresh}
          onCancel={() => setPlanModal(null)}
        />
      )}
    </div>
  );
}
