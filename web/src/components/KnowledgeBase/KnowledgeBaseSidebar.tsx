import { useEffect, useRef, useState } from 'react';
import type { KnowledgeBaseSummary } from '../../types';

interface SidebarProps {
  summary: KnowledgeBaseSummary;
  selected: { repo: string; doc: string } | null;
  onSelect: (sel: { repo: string; doc: string }) => void;
}

interface FlatDoc {
  repo: string;
  doc: string;
  human_edited: boolean;
}

export function KnowledgeBaseSidebar({ summary, selected, onSelect }: SidebarProps) {
  const flatDocs: FlatDoc[] = summary.repos.flatMap((repo) =>
    repo.docs.map((d) => ({ repo: repo.name, doc: d.name, human_edited: d.human_edited })),
  );

  const initialFocusIndex = (() => {
    if (!selected) return 0;
    const idx = flatDocs.findIndex((d) => d.repo === selected.repo && d.doc === selected.doc);
    return idx >= 0 ? idx : 0;
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

  let flatIdx = 0;
  return (
    <nav
      className="w-64 overflow-auto"
      style={{ borderRight: '1px solid var(--bg3)' }}
      onKeyDown={onKeyDown}
    >
      {summary.repos.map((repo) => (
        <div key={repo.name} className="py-2">
          <h3
            className="px-3 py-1 text-xs font-semibold uppercase"
            style={{ color: 'var(--grey1)', margin: 0 }}
          >
            {repo.name}
          </h3>
          <ul>
            {repo.docs.map((doc) => {
              const myIdx = flatIdx++;
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
                      if (selected?.repo === repo.name && selected?.doc === doc.name) {
                        return;
                      }
                      onSelect({ repo: repo.name, doc: doc.name });
                    }}
                    onFocus={() => setFocusedIdx(myIdx)}
                    className={`w-full text-left px-3 py-1 text-sm flex items-center justify-between ${
                      isSelected ? '' : 'hover:bg-[var(--bg1)]'
                    }`}
                    style={{
                      color: isSelected ? 'var(--fg)' : 'var(--grey1)',
                      backgroundColor: isSelected ? 'var(--bg1)' : 'transparent',
                    }}
                  >
                    <span>{doc.name}</span>
                    {doc.human_edited && (
                      <span
                        className="ml-2 text-xs px-1 rounded"
                        style={{ backgroundColor: 'var(--yellow)', color: 'var(--bg0)' }}
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
      ))}
    </nav>
  );
}
