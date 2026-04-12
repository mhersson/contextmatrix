import { memo } from 'react';

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
}

export const AutomationCheckboxes = memo(function AutomationCheckboxes({
  autonomous, featureBranch, createPR,
  onAutonomousChange, onFeatureBranchChange, onCreatePRChange,
  branchName, prUrl, reviewAttempts,
  baseBranch, onBaseBranchChange, branches, branchesLoading, branchesError,
}: AutomationCheckboxesProps) {
  return (
    <div>
      <label className="block text-xs text-[var(--grey1)] mb-2">Automation</label>
      <div className="space-y-2">
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={autonomous}
            onChange={(e) => onAutonomousChange(e.target.checked)}
            className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
          />
          <span className="text-sm text-[var(--fg)]">Autonomous mode</span>
        </label>
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
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
            checked={createPR}
            disabled={!featureBranch}
            onChange={(e) => onCreatePRChange(e.target.checked)}
            className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
          />
          <span className="text-sm text-[var(--fg)]">Create PR</span>
        </label>
        {autonomous && (
          <div>
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
