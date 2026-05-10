import { lazy, Suspense, useState } from 'react';
import { api } from '../../api/client';
import { useTheme } from '../../hooks/useTheme';
import type { KnowledgeDocResponse, KnowledgeRepoSummary } from '../../types';
import { KnowledgeDocEditor } from './KnowledgeDocEditor';
import { formatRelativeTime } from '../CardPanel/utils';

const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

interface ViewerProps {
  project: string;
  repo: string;
  doc: string;
  response: KnowledgeDocResponse;
  repoSummary?: KnowledgeRepoSummary;
  onSaved: () => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
  refreshing?: boolean;
  onRefreshClick?: (repo: string) => void;
}

const docTitleGlyph = (
  <svg
    className="w-4 h-4 flex-shrink-0"
    style={{ color: 'var(--grey1)' }}
    viewBox="0 0 16 16"
    fill="currentColor"
    aria-hidden="true"
  >
    <path d="M2 1.75C2 .784 2.784 0 3.75 0h5.586c.464 0 .909.184 1.237.513l2.914 2.914c.329.328.513.773.513 1.237v9.586A1.75 1.75 0 0 1 12.25 16h-8.5A1.75 1.75 0 0 1 2 14.25Zm1.75-.25a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6h-2.75A1.75 1.75 0 0 1 8 4.25V1.5Zm6.75.062V4.25c0 .138.112.25.25.25h2.688l-.011-.013-2.914-2.914-.013-.011Z" />
  </svg>
);

const commitGlyph = (
  <svg className="w-2.5 h-2.5" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
    <path d="M11.93 8.5a4.002 4.002 0 0 1-7.86 0H.75a.75.75 0 0 1 0-1.5h3.32a4.002 4.002 0 0 1 7.86 0h3.32a.75.75 0 0 1 0 1.5Zm-1.43-.75a2.5 2.5 0 1 0-5 0 2.5 2.5 0 0 0 5 0Z" />
  </svg>
);

const editGlyph = (
  <svg className="w-3 h-3" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
    <path d="M11.013 1.427a1.75 1.75 0 0 1 2.474 0l1.086 1.086a1.75 1.75 0 0 1 0 2.474l-8.61 8.61c-.21.21-.47.364-.756.445l-3.251.93a.75.75 0 0 1-.927-.928l.929-3.25c.081-.286.235-.547.445-.758Z" />
  </svg>
);

const refreshGlyph = (
  <svg className="w-3 h-3" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
    <path d="M1.705 8.005a.75.75 0 0 1 .834.656 5.5 5.5 0 0 0 9.592 2.97l-1.204-1.204a.25.25 0 0 1 .177-.427h3.646a.25.25 0 0 1 .25.25v3.646a.25.25 0 0 1-.427.177l-1.38-1.38A7.002 7.002 0 0 1 1.05 8.84a.75.75 0 0 1 .655-.834ZM8 2.5a5.487 5.487 0 0 0-4.131 1.869l1.204 1.204A.25.25 0 0 1 4.896 6H1.25A.25.25 0 0 1 1 5.75V2.104a.25.25 0 0 1 .427-.177l1.38 1.38A7.002 7.002 0 0 1 14.95 7.16a.75.75 0 0 1-1.49.178A5.5 5.5 0 0 0 8 2.5Z" />
  </svg>
);

function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

export function KnowledgeDocViewer({
  project,
  repo,
  doc,
  response,
  repoSummary,
  onSaved,
  onDirtyChange,
  refreshing,
  onRefreshClick,
}: ViewerProps) {
  const [editing, setEditing] = useState(false);
  const { theme } = useTheme();

  if (editing) {
    return (
      <KnowledgeDocEditor
        initialContent={response.content}
        onCancel={() => {
          onDirtyChange?.(false);
          setEditing(false);
        }}
        onSave={async (content, signal) => {
          await api.putKnowledgeDoc(project, repo, doc, content, { signal });
          if (signal.aborted) return;
          await onSaved();
          if (signal.aborted) return;
          onDirtyChange?.(false);
          setEditing(false);
        }}
        onDirtyChange={onDirtyChange}
      />
    );
  }

  const sha = response.meta.last_built_commit
    ? shortSha(response.meta.last_built_commit)
    : '';
  const builtAgo = repoSummary?.last_built_at
    ? formatRelativeTime(repoSummary.last_built_at)
    : null;
  const handEdited = response.meta.human_edited;

  return (
    <div className="flex flex-col h-full">
      <header
        className="flex flex-wrap items-start gap-x-4 gap-y-3 px-7 py-5"
        style={{ borderBottom: '1px solid var(--bg3)', backgroundColor: 'var(--bg0)' }}
      >
        <div className="flex-1 min-w-0 flex flex-col gap-2" style={{ flexBasis: '340px' }}>
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="chip-pill"
              style={{
                backgroundColor: 'color-mix(in srgb, var(--purple) 22%, transparent)',
                color: 'var(--purple)',
              }}
            >
              {repo}
            </span>
            <span
              className="chip-pill"
              style={{
                backgroundColor: refreshing
                  ? 'color-mix(in srgb, var(--aqua) 22%, transparent)'
                  : 'color-mix(in srgb, var(--green) 22%, transparent)',
                color: refreshing ? 'var(--aqua)' : 'var(--green)',
              }}
              role="status"
              aria-live="polite"
            >
              {refreshing ? (
                <span className="kb-spin">{refreshGlyph}</span>
              ) : (
                <span
                  className="inline-block rounded-full"
                  style={{ width: 5, height: 5, backgroundColor: 'currentColor' }}
                  aria-hidden="true"
                />
              )}
              {refreshing ? 'refreshing' : 'synced'}
            </span>
            {sha && (
              <span
                className="chip-pill"
                style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}
                title={response.meta.last_built_commit}
              >
                {commitGlyph}
                {sha}
              </span>
            )}
            {handEdited && (
              <span
                className="chip-pill"
                style={{
                  backgroundColor: 'transparent',
                  color: 'var(--yellow)',
                  borderColor: 'var(--yellow)',
                }}
                aria-label="This doc has been hand-edited"
              >
                {editGlyph}
                hand-edited
              </span>
            )}
          </div>
          <div className="flex items-center gap-2.5">
            {docTitleGlyph}
            <h1 className="kb-doc-title truncate" title={doc}>
              {doc}
            </h1>
          </div>
          <div
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: '11px',
              color: 'var(--grey1)',
              letterSpacing: '0.02em',
            }}
          >
            <span>{repo}</span>
            {builtAgo && (
              <>
                <span style={{ color: 'var(--bg4)', margin: '0 6px' }}>·</span>
                <span>last built {builtAgo}</span>
              </>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 ml-auto shrink-0">
          <button
            type="button"
            onClick={() => setEditing(true)}
            disabled={refreshing}
            title={refreshing ? 'Edit disabled while refresh is in progress' : undefined}
            className="px-3 py-1.5 rounded text-sm transition-colors"
            style={{
              border: '1px solid var(--bg3)',
              color: refreshing ? 'var(--grey1)' : 'var(--fg)',
              backgroundColor: 'transparent',
              cursor: refreshing ? 'not-allowed' : 'pointer',
              opacity: refreshing ? 0.6 : 1,
            }}
          >
            Edit
          </button>
          {onRefreshClick && (
            <button
              type="button"
              onClick={() => onRefreshClick(repo)}
              disabled={refreshing}
              className="px-3 py-1.5 rounded text-sm font-medium inline-flex items-center gap-2 hover:opacity-90 transition-opacity"
              style={{
                backgroundColor: 'var(--bg-green)',
                color: 'var(--green)',
                cursor: refreshing ? 'not-allowed' : 'pointer',
                opacity: refreshing ? 0.6 : 1,
              }}
              aria-label={`Refresh ${repo}`}
            >
              {refreshGlyph}
              <span>Refresh</span>
            </button>
          )}
        </div>
      </header>
      <div className="flex-1 overflow-auto">
        <article className="px-8 pt-7 pb-4" data-color-mode={theme}>
          <Suspense fallback={null}>
            <MarkdownPreview source={response.content} skipHtml className="bf-markdown" />
          </Suspense>
        </article>
      </div>
      <footer
        className="kb-meta-footer flex items-center gap-2.5 px-7 py-2.5 flex-wrap"
        style={{ borderTop: '1px solid var(--bg3)', backgroundColor: 'var(--bg0)' }}
      >
        {sha && (
          <>
            <span className="kb-meta-label">commit</span>
            <span className="kb-meta-sha" title={response.meta.last_built_commit}>
              {sha}
            </span>
          </>
        )}
        {builtAgo && (
          <>
            <span className="kb-meta-sep">·</span>
            <span className="kb-meta-label">built</span>
            <span>{builtAgo}</span>
          </>
        )}
        {repoSummary && (
          <>
            <span className="kb-meta-sep">·</span>
            <span className="kb-meta-label">repo</span>
            <span style={{ color: 'var(--grey2)' }}>{repo}</span>
            <span className="kb-meta-sep">·</span>
            <span className="kb-meta-label">docs</span>
            <span>{repoSummary.docs.length}</span>
          </>
        )}
        {handEdited && (
          <>
            <span className="kb-meta-sep">·</span>
            <span style={{ color: 'var(--yellow)' }}>locally edited</span>
          </>
        )}
      </footer>
    </div>
  );
}
