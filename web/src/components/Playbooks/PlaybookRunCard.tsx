import type { PlaybookDetail } from '../../types';
import { describeRun, isRunActive, runStatusChip } from './playbookUtils';

interface PlaybookRunCardProps {
  detail: PlaybookDetail;
  /** Branches present in every repository the playbook spans. */
  branches: string[];
  branchesLoading: boolean;
  branchesError: boolean;
  onToggleRunnable: (next: boolean) => void;
  onSaveBaseBranch: (value: string) => void;
  onRun: () => void;
  onStop: () => void;
}

const pillStyle = {
  fontFamily: 'var(--font-mono)', fontSize: '10px', padding: '2px 8px', borderRadius: '999px', fontWeight: 500,
} as const;

/** Side-panel card holding every playbook-run control: the runnable switch,
 * the base branch, the status line, Run/Stop and the final compare links.
 * Controls live here, on the region they act on, never in a top bar. */
export function PlaybookRunCard({
  detail, branches, branchesLoading, branchesError, onToggleRunnable, onSaveBaseBranch, onRun, onStop,
}: PlaybookRunCardProps) {
  const runnable = detail.runnable === true;
  const active = isRunActive(detail.run);
  const baseBranch = detail.base_branch ?? '';
  // A stored base branch that is not in the shared list (loading, or gone
  // from one repository) stays selectable so the select never renders blank.
  const options = baseBranch && !branches.includes(baseBranch) ? [baseBranch, ...branches] : branches;

  const chip = detail.run ? runStatusChip(detail.run.status) : null;

  return (
    <div className="pb-side-card">
      <h2 className="pb-eyebrow">Run</h2>

      <div className="bf-spread">
        <label className="bf-switch" title={active ? 'Stop the run to change this' : undefined}>
          <input
            type="checkbox"
            aria-label="Make runnable"
            checked={runnable}
            disabled={active}
            onChange={(e) => onToggleRunnable(e.target.checked)}
          />
          <span>Make runnable</span>
        </label>
        {runnable && detail.branch && (
          <span className="font-mono text-xs" style={{ color: 'var(--aqua)' }}>{detail.branch}</span>
        )}
      </div>

      <div className="bf-spread mt-2">
        <span className="bf-switch-label">Base branch</span>
        <select
          aria-label="Base branch"
          value={baseBranch}
          onChange={(e) => onSaveBaseBranch(e.target.value)}
          disabled={active || branchesLoading}
          className="bf-input"
          style={{ width: 'auto', minWidth: '160px' }}
        >
          <option value="">Default branch</option>
          {options.map((b) => (
            <option key={b} value={b}>{b}</option>
          ))}
        </select>
      </div>
      {branchesError && (
        <div className="text-xs text-[var(--yellow)] -mt-1">Could not load branches</div>
      )}

      {runnable && (
        <div className="mt-3 pt-2.5" style={{ borderTop: '1px solid var(--bg1)' }}>
          <div className="flex items-center gap-2 flex-wrap">
            {chip && (
              <span className={detail.run?.status === 'running' ? 'pb-pulse' : undefined} style={{ ...pillStyle, backgroundColor: chip.bg, color: chip.color }}>
                {chip.label}
              </span>
            )}
            <span className="text-sm" style={{ color: 'var(--fg)' }}>{describeRun(detail)}</span>
          </div>

          <div className="mt-2 flex gap-2">
            {active ? (
              <button
                type="button"
                onClick={onStop}
                className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm font-medium"
              >
                Stop
              </button>
            ) : (
              <button
                type="button"
                onClick={onRun}
                className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-opacity text-sm font-medium inline-flex items-center gap-2"
              >
                <span aria-hidden="true">▶</span>
                <span>Run</span>
              </button>
            )}
          </div>

          {detail.run?.status === 'completed' && (detail.repos?.length ?? 0) > 0 && (
            <ul className="mt-2 flex flex-col gap-1">
              {detail.repos!.map((r) => (
                <li key={r.project}>
                  <a
                    href={r.compare_url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-sm"
                    style={{ color: 'var(--aqua)' }}
                  >
                    Open PR: {r.project}
                  </a>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
