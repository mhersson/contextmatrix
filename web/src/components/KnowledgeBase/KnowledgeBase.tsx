import { useNavigate, useParams } from 'react-router-dom';
import { api, errorMessage } from '../../api/client';
import { KnowledgeBaseSidebar } from './KnowledgeBaseSidebar';
import { KnowledgeDocViewer } from './KnowledgeDocViewer';
import { useUnsavedGuard } from './useUnsavedGuard';
import { useKnowledgeBaseData } from './useKnowledgeBaseData';

export function KnowledgeBase({ project }: { project: string }) {
  const navigate = useNavigate();
  const params = useParams();
  const splat = params['*'] ?? '';
  const [splatRepo, splatDoc] = splat.split('/');
  const selected =
    splatRepo && splatDoc
      ? { repo: decodeURIComponent(splatRepo), doc: decodeURIComponent(splatDoc) }
      : null;

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
  } = useKnowledgeBaseData(project, selected);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full" style={{ color: 'var(--grey1)' }}>
        <div className="text-sm">Loading knowledge base...</div>
      </div>
    );
  }

  if (summaryError) {
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
        <p>No knowledge base yet for this project.</p>
        <p className="mt-2 text-sm">
          Run{' '}
          <code
            className="px-1 rounded"
            style={{ backgroundColor: 'var(--bg1)', color: 'var(--fg)' }}
          >
            /contextmatrix:refresh-knowledge --project {project}
          </code>{' '}
          in your Claude Code session to build one.
        </p>
      </div>
    );
  }

  const reload = async () => {
    try {
      if (selected) {
        const refreshed = await api.getKnowledgeDoc(project, selected.repo, selected.doc);
        setDocContent(refreshed);
        setDocError(null);
      }
      const fullSummary = await api.getKnowledgeBase(project);
      setSummary(fullSummary);
    } catch (err) {
      setDocError(errorMessage(err));
    }
  };

  return (
    <div className="flex h-full">
      <KnowledgeBaseSidebar summary={summary} selected={selected} onSelect={guard} />
      <div className="flex-1 overflow-auto">
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
            onSaved={reload}
            onDirtyChange={setDirty}
          />
        ) : (
          <div className="p-6" style={{ color: 'var(--grey1)' }}>
            Select a doc.
          </div>
        )}
      </div>
      {modal}
    </div>
  );
}
