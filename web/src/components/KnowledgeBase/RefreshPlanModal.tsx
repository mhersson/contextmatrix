import { useEffect, useRef, useState } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import type { RefreshPlan } from '../../types';

interface Props {
  plan: RefreshPlan;
  repo: string;
  onConfirm: (overwriteDocs: string[]) => void | Promise<void>;
  onCancel: () => void;
}

export function RefreshPlanModal({ plan, repo, onConfirm, onCancel }: Props) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const confirmBtnRef = useRef<HTMLButtonElement>(null);

  // overwriteOn tracks per-doc consent for human_edited items only. Non-edited
  // docs are always rebuilt (no checkbox needed) and never appear in the
  // overwrite_docs list.
  const [overwriteOn, setOverwriteOn] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const item of plan.items) {
      if (item.human_edited) init[item.doc] = false;
    }
    return init;
  });

  // Tracks an in-flight onConfirm call so a fast double-click cannot fire
  // two POSTs to the trigger endpoint before the parent unmounts the modal.
  const [submitting, setSubmitting] = useState(false);

  // Total cost across all plan items regardless of overwrite consent.
  const totalCost = plan.items.reduce((sum, i) => sum + i.estimated_cost_usd, 0);

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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        aria-hidden="true"
        onClick={onCancel}
      />

      {/* Dialog panel */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="refresh-plan-title"
        className="relative z-10 rounded p-6 max-w-md w-full"
        style={{ backgroundColor: 'var(--bg2)', color: 'var(--fg)' }}
      >
        <h2 id="refresh-plan-title" className="text-lg mb-2">
          Refresh knowledge base — {repo}
        </h2>
        <p className="text-sm mb-4" style={{ color: 'var(--grey1)' }}>
          {plan.items.length} docs scheduled. Estimated cost: ${totalCost.toFixed(2)}.
        </p>
        <ul className="space-y-2 mb-4">
          {plan.items.map((item) => (
            <li key={item.doc} className="flex items-center gap-2 text-sm">
              {item.human_edited ? (
                <input
                  type="checkbox"
                  aria-label={item.doc}
                  checked={!!overwriteOn[item.doc]}
                  onChange={(e) =>
                    setOverwriteOn((s) => ({ ...s, [item.doc]: e.target.checked }))
                  }
                />
              ) : (
                <span
                  aria-label={`${item.doc} rebuilds automatically`}
                  title="rebuilt automatically"
                  className="text-xs uppercase tracking-wide select-none"
                  style={{ color: 'var(--grey1)' }}
                >
                  auto
                </span>
              )}
              <span className="flex-1">
                {item.doc} — {item.reason}{item.human_edited ? ' (edited by you)' : ''}
              </span>
              <span style={{ color: 'var(--grey1)' }}>${item.estimated_cost_usd.toFixed(2)}</span>
            </li>
          ))}
        </ul>
        <p className="text-xs mb-4" style={{ color: 'var(--grey1)' }}>
          Check to overwrite human-edited docs. Other docs always rebuild.
        </p>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1 rounded"
            style={{ color: 'var(--fg)' }}
          >
            Cancel
          </button>
          <button
            ref={confirmBtnRef}
            type="button"
            onClick={handleConfirm}
            disabled={confirmDisabled}
            className="px-3 py-1 rounded disabled:opacity-50 disabled:cursor-not-allowed"
            style={{ backgroundColor: 'var(--green)', color: 'var(--bg-dim)' }}
          >
            Refresh
          </button>
        </div>
      </div>
    </div>
  );
}
