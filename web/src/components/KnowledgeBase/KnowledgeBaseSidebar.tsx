import { useEffect, useMemo, useRef, useState } from 'react';
import type { KnowledgeBaseSummary, RefreshJobStatus } from '../../types';
import { formatRelativeTime } from '../CardPanel/utils';

interface SidebarProps {
  summary: KnowledgeBaseSummary;
  selected: { repo: string; doc: string } | null;
  onSelect: (sel: { repo: string; doc: string }) => void;
  onRefreshClick?: (repo: string) => void;
  refreshStatusByRepo?: Record<string, RefreshJobStatus>;
}

interface FlatDoc {
  repo: string;
  doc: string;
  human_edited: boolean;
}

const repoIcon = (
  <svg className="w-3 h-3 flex-shrink-0 opacity-70" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
    <path d="M2 2.5A2.5 2.5 0 0 1 4.5 0h8.75a.75.75 0 0 1 .75.75v12.5a.75.75 0 0 1-.75.75h-2.5a.75.75 0 0 1 0-1.5h1.75v-2h-8a1 1 0 0 0-.714 1.7.75.75 0 1 1-1.072 1.05A2.495 2.495 0 0 1 2 11.5Zm10.5-1h-8a1 1 0 0 0-1 1v6.708A2.486 2.486 0 0 1 4.5 9h8Z" />
  </svg>
);

const docIcon = (
  <svg className="w-2.5 h-2.5 flex-shrink-0 opacity-60" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
    <path d="M2 1.75C2 .784 2.784 0 3.75 0h5.586c.464 0 .909.184 1.237.513l2.914 2.914c.329.328.513.773.513 1.237v9.586A1.75 1.75 0 0 1 12.25 16h-8.5A1.75 1.75 0 0 1 2 14.25Zm1.75-.25a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6h-2.75A1.75 1.75 0 0 1 8 4.25V1.5Zm6.75.062V4.25c0 .138.112.25.25.25h2.688l-.011-.013-2.914-2.914-.013-.011Z" />
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

export function KnowledgeBaseSidebar({
  summary,
  selected,
  onSelect,
  onRefreshClick,
  refreshStatusByRepo,
}: SidebarProps) {
  const flatDocs: FlatDoc[] = useMemo(
    () =>
      summary.repos.flatMap((repo) =>
        repo.docs.map((d) => ({ repo: repo.name, doc: d.name, human_edited: d.human_edited })),
      ),
    [summary],
  );

  const idxMap = useMemo(
    () => new Map(flatDocs.map((d, i) => [`${d.repo}/${d.doc}`, i])),
    [flatDocs],
  );

  const initialFocusIndex = (() => {
    if (!selected) return 0;
    return idxMap.get(`${selected.repo}/${selected.doc}`) ?? 0;
  })();

  const [focusedIdx, setFocusedIdx] = useState(initialFocusIndex);
  const buttonsRef = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    const active = document.activeElement;
    if (
      active?.tagName === 'BUTTON' &&
      buttonsRef.current.includes(active as HTMLButtonElement)
    ) {
      buttonsRef.current[focusedIdx]?.focus();
    }
  }, [focusedIdx]);

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (flatDocs.length === 0) return;
    let next: number;
    if (e.key === 'ArrowDown') next = Math.min(focusedIdx + 1, flatDocs.length - 1);
    else if (e.key === 'ArrowUp') next = Math.max(focusedIdx - 1, 0);
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = flatDocs.length - 1;
    else return;
    e.preventDefault();
    setFocusedIdx(next);
    buttonsRef.current[next]?.focus();
  };

  const totalDocs = summary.repos.reduce((sum, r) => sum + r.docs.length, 0);
  const refreshingCount = summary.repos.filter((r) => {
    const s = refreshStatusByRepo?.[r.name]?.state;
    return s === 'planning' || s === 'running';
  }).length;
  const inSyncCount = summary.repos.length - refreshingCount;
  const repoLabel = summary.repos.length === 1 ? 'repo' : 'repos';
  const docLabel = totalDocs === 1 ? 'doc' : 'docs';

  return (
    <nav
      className="w-72 h-full min-h-0 flex flex-col overflow-y-auto"
      style={{
        borderRight: '1px solid var(--bg3)',
        backgroundColor: 'var(--bg0)',
      }}
      onKeyDown={onKeyDown}
    >
      <div
        className="flex items-center justify-between px-4 py-3"
        style={{
          borderBottom: '1px solid var(--bg3)',
          backgroundColor: 'var(--bg-dim)',
        }}
      >
        <span className="section-eyebrow">Knowledge base</span>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: '10.5px',
            color: 'var(--grey1)',
            letterSpacing: '0.02em',
          }}
        >
          {summary.repos.length} {repoLabel} · {totalDocs} {docLabel}
        </span>
      </div>
      {summary.repos.map((repo) => {
        const status = refreshStatusByRepo?.[repo.name];
        const isRefreshing = status?.state === 'planning' || status?.state === 'running';
        const builtAgo = repo.last_built_at ? formatRelativeTime(repo.last_built_at) : '—';
        const sha = repo.last_built_commit ? shortSha(repo.last_built_commit) : '';
        return (
          <div
            key={repo.name}
            className="px-4 pt-4 pb-2"
            style={{ borderBottom: '1px solid var(--bg3)' }}
          >
            <div className="flex items-center justify-between gap-2 mb-2">
              <h3
                className="flex items-center gap-1.5 m-0 font-medium min-w-0"
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: '12px',
                  letterSpacing: '0.01em',
                  color: 'var(--fg)',
                }}
                title={repo.name}
              >
                {repoIcon}
                <span className="truncate">{repo.name}</span>
              </h3>
              {isRefreshing ? (
                <span
                  className="inline-flex items-center gap-1 px-2 rounded-md"
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: '10.5px',
                    color: 'var(--aqua)',
                    backgroundColor: 'var(--bg-blue)',
                    height: '22px',
                    lineHeight: 1,
                  }}
                  aria-label={`Refreshing ${repo.name}`}
                >
                  <span className="kb-spin">{refreshGlyph}</span>
                  {status?.docs_done ?? 0}/{status?.docs_total ?? '?'}
                </span>
              ) : (
                onRefreshClick && (
                  <button
                    type="button"
                    onClick={() => onRefreshClick(repo.name)}
                    className="inline-flex items-center gap-1 px-2 rounded-md hover:opacity-85 transition-opacity"
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '10.5px',
                      color: 'var(--aqua)',
                      backgroundColor: 'var(--bg-blue)',
                      height: '22px',
                      letterSpacing: '0.02em',
                      lineHeight: 1,
                    }}
                    aria-label={`Refresh ${repo.name}`}
                  >
                    {refreshGlyph}
                    Refresh
                  </button>
                )
              )}
            </div>
            <div
              className="flex items-center gap-1.5 mb-2"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: '10.5px',
                color: 'var(--grey1)',
                lineHeight: 1,
              }}
            >
              <span>built {builtAgo}</span>
              {sha && (
                <>
                  <span style={{ color: 'var(--bg4)' }}>·</span>
                  <span style={{ color: 'var(--grey2)' }}>{sha}</span>
                </>
              )}
              <span style={{ color: 'var(--bg4)' }}>·</span>
              <span>
                {repo.docs.length} {repo.docs.length === 1 ? 'doc' : 'docs'}
              </span>
            </div>
            <ul className="-mx-4 list-none p-0 m-0">
              {repo.docs.map((doc) => {
                const myIdx = idxMap.get(`${repo.name}/${doc.name}`) ?? -1;
                const isSelected = selected?.repo === repo.name && selected.doc === doc.name;
                return (
                  <li key={doc.name}>
                    <button
                      type="button"
                      ref={(el) => {
                        buttonsRef.current[myIdx] = el;
                      }}
                      tabIndex={myIdx === focusedIdx ? 0 : -1}
                      aria-current={isSelected ? 'true' : undefined}
                      onClick={() => {
                        if (selected?.repo === repo.name && selected?.doc === doc.name) return;
                        onSelect({ repo: repo.name, doc: doc.name });
                      }}
                      onFocus={() => setFocusedIdx(myIdx)}
                      className="kb-doc-row"
                    >
                      {docIcon}
                      <span className="kb-doc-name">{doc.name}</span>
                      <span className="kb-doc-ts" aria-hidden="true">
                        {builtAgo}
                      </span>
                      {doc.human_edited && (
                        <span
                          className="inline-flex items-center px-1.5 rounded-full"
                          style={{
                            fontFamily: 'var(--font-mono)',
                            fontSize: '9.5px',
                            color: 'var(--yellow)',
                            border: '1px solid var(--yellow)',
                            backgroundColor: 'transparent',
                            lineHeight: 1.4,
                          }}
                          aria-label="Doc has been hand-edited"
                        >
                          edited
                        </span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        );
      })}
      <div
        className="kb-meta-footer flex items-center px-4 py-2.5 mt-auto"
        style={{
          borderTop: '1px solid var(--bg3)',
          backgroundColor: 'var(--bg-dim)',
        }}
      >
        {refreshingCount > 0 ? (
          <span style={{ color: 'var(--aqua)' }}>
            {refreshingCount} refreshing · {inSyncCount}/{summary.repos.length} in sync
          </span>
        ) : (
          <span>
            {inSyncCount}/{summary.repos.length} in sync · {totalDocs} {docLabel} indexed
          </span>
        )}
      </div>
    </nav>
  );
}
