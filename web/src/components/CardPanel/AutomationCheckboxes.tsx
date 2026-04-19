import { memo, useState } from 'react';

const MAX_REVIEW_ATTEMPTS = 2;

interface AutomationCheckboxesProps {
  autonomous: boolean;
  featureBranch: boolean;
  createPR: boolean;
  onAutonomousChange: (value: boolean) => void;
  onFeatureBranchChange: (value: boolean) => void;
  onCreatePRChange: (value: boolean) => void;
  branchName?: string;
  prUrl?: string;
  reviewAttempts?: number;
  baseBranch?: string;
  onBaseBranchChange: (value: string) => void;
  branches: string[];
  branchesLoading?: boolean;
  branchesError?: boolean;
  canRun?: boolean;
  onRun?: () => void | Promise<void>;
}

export const AutomationCheckboxes = memo(function AutomationCheckboxes({
  autonomous, featureBranch, createPR,
  onAutonomousChange, onFeatureBranchChange, onCreatePRChange,
  branchName, prUrl, reviewAttempts,
  baseBranch, onBaseBranchChange, branches, branchesLoading, branchesError,
  canRun, onRun,
}: AutomationCheckboxesProps) {
  const [runLoading, setRunLoading] = useState(false);

  const handleRun = async () => {
    if (!onRun) return;
    setRunLoading(true);
    try { await onRun(); } finally { setRunLoading(false); }
  };

  const showRunControls = canRun && onRun != null;

  return (
    <div>
      <div className="flex items-start justify-between gap-2">
        <div className="space-y-2 flex-1">
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              aria-label="Autonomous mode"
              checked={autonomous}
              onChange={(e) => onAutonomousChange(e.target.checked)}
              className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
            />
            <span className="text-sm text-[var(--fg)]">Autonomous mode</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              aria-label="Feature branch"
              checked={featureBranch}
              onChange={(e) => onFeatureBranchChange(e.target.checked)}
              className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
            />
            <span className="text-sm text-[var(--fg)]">Feature branch</span>
          </label>
          <label
            className={`flex items-center gap-2 ${featureBranch ? 'cursor-pointer' : 'opacity-50 cursor-not-allowed'}`}
            title={featureBranch ? undefined : 'Requires Feature Branch'}
          >
            <input
              type="checkbox"
              aria-label="Create PR"
              checked={createPR}
              disabled={!featureBranch}
              onChange={(e) => onCreatePRChange(e.target.checked)}
              className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
            />
            <span className="text-sm text-[var(--fg)]">Create PR</span>
          </label>
        </div>
        {showRunControls && (
          <button
            type="button"
            onClick={handleRun}
            disabled={runLoading}
            className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-opacity text-sm disabled:opacity-50 shrink-0"
          >
            {runLoading ? 'Starting...' : autonomous ? 'Run Auto' : 'Run HITL'}
          </button>
        )}
      </div>
      <div className="mt-3">
        <label className="block text-xs text-[var(--grey1)] mb-1">Base branch</label>
        <select
          aria-label="Base branch"
          value={baseBranch ?? ''}
          onChange={(e) => onBaseBranchChange(e.target.value)}
          disabled={branchesLoading}
          className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)] text-sm disabled:opacity-50"
        >
          <option value="">Default branch</option>
          {branches.map((b) => (
            <option key={b} value={b}>{b}</option>
          ))}
        </select>
        {branchesError && (
          <div className="text-xs text-[var(--yellow)] mt-1">Could not load branches</div>
        )}
      </div>
      {branchName && (
        <div className="mt-2 text-xs font-mono px-2 py-1 rounded bg-[var(--bg2)] text-[var(--aqua)]">
          {branchName}
        </div>
      )}
      {prUrl && /^https?:\/\//.test(prUrl) && (
        <div className="mt-1">
          <a
            href={prUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-[var(--aqua)] hover:underline"
          >
            {prUrl}
          </a>
        </div>
      )}
      {reviewAttempts != null && reviewAttempts > 0 && (
        <div className="mt-1 text-xs text-[var(--yellow)]">
          Review attempts: {reviewAttempts}/{MAX_REVIEW_ATTEMPTS}
        </div>
      )}
    </div>
  );
});
