import { useEffect, useRef, useState } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import type { RefreshPlan } from '../../types';

interface Props {
  plan: RefreshPlan;
  repo: string;
  onConfirm: (overwriteDocs: string[]) => void | Promise<void>;
  onCancel: () => void;
}

const docIcon = (
  <svg
    className="w-3 h-3 flex-shrink-0 opacity-60"
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

export function RefreshPlanModal({ plan, repo, onConfirm, onCancel }: Props) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const confirmBtnRef = useRef<HTMLButtonElement>(null);

  const [overwriteOn, setOverwriteOn] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const item of plan.items) {
      if (item.human_edited) init[item.doc] = false;
    }
    return init;
  });
  const [submitting, setSubmitting] = useState(false);

  useFocusTrap(dialogRef, true, confirmBtnRef);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onCancel]);

  const handleConfirm = async () => {
    if (submitting || plan.items.length === 0) return;
    const list = Object.entries(overwriteOn)
      .filter(([, v]) => v)
      .map(([k]) => k);
    setSubmitting(true);
    try {
      await onConfirm(list);
    } finally {
      setSubmitting(false);
    }
  };

  const confirmDisabled = submitting || plan.items.length === 0;
  const editedCount = plan.items.filter((i) => i.human_edited).length;
  const headSha = plan.head_commit ? plan.head_commit.slice(0, 7) : '';
  const docLabel = plan.items.length === 1 ? 'doc' : 'docs';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={onCancel} />

      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="refresh-plan-title"
        className="relative z-10 rounded-lg w-full overflow-hidden flex flex-col"
        style={{
          maxWidth: '32rem',
          maxHeight: '85vh',
          backgroundColor: 'var(--bg2)',
          color: 'var(--fg)',
          border: '1px solid var(--bg3)',
        }}
      >
        <header
          className="px-6 pt-5 pb-4 flex flex-col gap-2"
          style={{ borderBottom: '1px solid var(--bg3)', backgroundColor: 'var(--bg-dim)' }}
        >
          <div className="flex items-center gap-2 flex-wrap">
            <span className="section-eyebrow">Refresh plan</span>
            <span
              className="chip-pill"
              style={{
                backgroundColor: 'color-mix(in srgb, var(--purple) 22%, transparent)',
                color: 'var(--purple)',
              }}
            >
              {repo}
            </span>
            {headSha && (
              <span
                className="chip-pill"
                style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}
                title={plan.head_commit}
              >
                {commitGlyph}
                {headSha}
              </span>
            )}
          </div>
          <h2 id="refresh-plan-title" className="kb-doc-title" style={{ fontSize: '20px' }}>
            {plan.items.length === 0
              ? 'Nothing to refresh'
              : `Rebuild ${plan.items.length} ${docLabel}`}
          </h2>
          <p
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: '11px',
              color: 'var(--grey1)',
              letterSpacing: '0.02em',
            }}
          >
            {editedCount > 0
              ? `${editedCount} hand-edited · check to overwrite local changes`
              : 'all docs rebuild automatically'}
          </p>
        </header>

        <div className="flex-1 overflow-y-auto" style={{ minHeight: 0 }}>
          {plan.items.length === 0 ? (
            <div
              className="px-6 py-8 text-center"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: '11.5px',
                color: 'var(--grey1)',
              }}
            >
              No documents are scheduled for rebuild.
            </div>
          ) : (
            <ul className="m-0 list-none p-0">
              {plan.items.map((item) => (
                <li
                  key={item.doc}
                  className="flex items-center gap-3 px-6 py-2.5"
                  style={{ borderBottom: '1px solid var(--bg3)' }}
                >
                  {docIcon}
                  <div className="flex-1 min-w-0 flex flex-col gap-0.5">
                    <span
                      style={{
                        fontFamily: 'var(--font-mono)',
                        fontSize: '12px',
                        color: 'var(--fg)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {item.doc}
                    </span>
                    <span
                      style={{
                        fontFamily: 'var(--font-mono)',
                        fontSize: '10.5px',
                        color: 'var(--grey1)',
                        letterSpacing: '0.02em',
                      }}
                    >
                      {item.reason}
                    </span>
                  </div>
                  {item.human_edited ? (
                    <div className="flex items-center gap-2 flex-shrink-0">
                      <span
                        className="chip-pill"
                        style={{
                          backgroundColor: 'transparent',
                          color: 'var(--yellow)',
                          borderColor: 'var(--yellow)',
                        }}
                      >
                        hand-edited
                      </span>
                      <label
                        className="flex items-center gap-1.5 cursor-pointer select-none"
                        style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: '10.5px',
                          color: 'var(--grey1)',
                          letterSpacing: '0.02em',
                        }}
                      >
                        <input
                          type="checkbox"
                          aria-label={item.doc}
                          checked={!!overwriteOn[item.doc]}
                          onChange={(e) =>
                            setOverwriteOn((s) => ({ ...s, [item.doc]: e.target.checked }))
                          }
                          style={{ accentColor: 'var(--yellow)' }}
                        />
                        overwrite
                      </label>
                    </div>
                  ) : (
                    <span
                      className="chip-pill flex-shrink-0"
                      style={{
                        backgroundColor: 'color-mix(in srgb, var(--aqua) 22%, transparent)',
                        color: 'var(--aqua)',
                      }}
                      aria-label={`${item.doc} rebuilds automatically`}
                      title="rebuilt automatically"
                    >
                      auto
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        <footer
          className="flex items-center justify-end gap-2 px-6 py-3.5"
          style={{ borderTop: '1px solid var(--bg3)', backgroundColor: 'var(--bg-dim)' }}
        >
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 rounded text-sm transition-colors"
            style={{
              border: '1px solid var(--bg3)',
              color: 'var(--fg)',
              backgroundColor: 'transparent',
            }}
          >
            Cancel
          </button>
          <button
            ref={confirmBtnRef}
            type="button"
            onClick={handleConfirm}
            disabled={confirmDisabled}
            className="px-3 py-1.5 rounded text-sm font-medium transition-opacity hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed"
            style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)' }}
          >
            Refresh
          </button>
        </footer>
      </div>
    </div>
  );
}
