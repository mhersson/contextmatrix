import { isSafeHttpUrl } from './utils';

const MAX_REVIEW_ATTEMPTS = 2;

interface AutomationCheckboxesProps {
  autonomous: boolean;
  useOpusOrchestrator: boolean;
  featureBranch: boolean;
  createPR: boolean;
  onAutonomousChange: (value: boolean) => void;
  onUseOpusOrchestratorChange: (value: boolean) => void;
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
  disabled?: boolean;
  forcedFeatureBranch?: boolean;
  forcedCreatePR?: boolean;
  onClearForcedFeatureBranch?: () => void;
  onClearForcedCreatePR?: () => void;
  /**
   * `'edit'` (default) shows the live branch name / PR url next to the
   * relevant checkboxes. `'create'` (used by CreateCardPanel) shows
   * placeholder hints — there's nothing to run yet.
   */
  mode?: 'edit' | 'create';
  /**
   * Optional override for the locked-state banner text. Defaults to the
   * runner-attached message when omitted; callers pass a state-specific
   * message (e.g. "Automation can only be edited in todo") when the lock
   * is driven by the card's lifecycle rather than an active runner.
   */
  lockedReason?: string;
}

/**
 * Automation rail — mirrors the design mock's `.spread` rows
 * (`/tmp/card-panel-explorer.html:2003-2048`). Each row puts the control
 * label on the left and a hint/value on the right:
 *
 *   [☐ Autonomous mode]            HITL — human replies in chat
 *   [☐ Opus as orchestrator]      → Sonnet (default)
 *   [☐ Feature branch]            ctxmax-456/foo
 *   [☐ Create pull request]       PR #482 ↗
 *   Base branch                   [main ▾]
 *   1 review attempt · max 2
 *   🔒 Automation locked during remote run        (when disabled)
 *
 * Run-status info lives inline with each row — no separate "Run status"
 * card. The Opus hint is yellow when ticked, grey otherwise. The autonomous
 * hint is uncolored (just `.bf-hint` defaults).
 */
export function AutomationCheckboxes({
  autonomous, useOpusOrchestrator, featureBranch, createPR,
  onAutonomousChange, onUseOpusOrchestratorChange, onFeatureBranchChange, onCreatePRChange,
  branchName, prUrl, reviewAttempts,
  baseBranch, onBaseBranchChange, branches, branchesLoading, branchesError,
  disabled = false,
  forcedFeatureBranch = false, forcedCreatePR = false,
  onClearForcedFeatureBranch, onClearForcedCreatePR,
  mode = 'edit',
  lockedReason,
}: AutomationCheckboxesProps) {
  const creating = mode === 'create';
  const prDisplay = formatPrLink(prUrl);

  return (
    <div className={`bf-auto-stack ${disabled ? 'opacity-60' : ''}`}>
      {/* Autonomous mode */}
      <div className="bf-spread">
        <label className="bf-switch">
          <input
            type="checkbox"
            aria-label="Autonomous mode"
            checked={autonomous}
            disabled={disabled}
            onChange={(e) => onAutonomousChange(e.target.checked)}
          />
          <span>Autonomous mode</span>
        </label>
        <span className="bf-hint">
          {autonomous ? 'no human-in-the-loop' : 'human-in-the-loop'}
        </span>
      </div>

      {/* Opus orchestrator */}
      <div className="bf-spread">
        <label className="bf-switch">
          <input
            type="checkbox"
            aria-label="Opus as orchestrator"
            checked={useOpusOrchestrator}
            disabled={disabled}
            onChange={(e) => onUseOpusOrchestratorChange(e.target.checked)}
          />
          <span>Opus as orchestrator</span>
        </label>
        <span className="bf-hint" style={{ color: useOpusOrchestrator ? 'var(--yellow)' : 'var(--grey1)' }}>
          {useOpusOrchestrator ? '→ Opus — deeper planning, higher cost' : '→ Sonnet (default)'}
        </span>
      </div>

      {/* Feature branch */}
      <div className="bf-spread">
        <div className="bf-switch-stack">
          <label className="bf-switch">
            <input
              type="checkbox"
              aria-label="Feature branch"
              checked={featureBranch}
              disabled={disabled}
              onChange={(e) => {
                onClearForcedFeatureBranch?.();
                onFeatureBranchChange(e.target.checked);
              }}
            />
            <span>Feature branch</span>
          </label>
          {forcedFeatureBranch && <ForcedBadge />}
        </div>
        <span className="bf-hint">
          {creating ? (
            <span className="italic" style={{ color: 'var(--grey0)' }}>auto-named from id</span>
          ) : branchName ? (
            <span style={{ color: 'var(--aqua)', fontFamily: 'var(--font-mono)' }}>{branchName}</span>
          ) : (
            <span className="italic" style={{ color: 'var(--grey0)' }}>not created yet</span>
          )}
        </span>
      </div>

      {/* Create pull request */}
      <div className="bf-spread">
        <div className="bf-switch-stack">
          <label
            className={`bf-switch ${featureBranch ? '' : 'opacity-50'}`}
            title={featureBranch ? undefined : 'Requires Feature Branch'}
          >
            <input
              type="checkbox"
              aria-label="Create PR"
              checked={createPR}
              disabled={!featureBranch || disabled}
              onChange={(e) => {
                onClearForcedCreatePR?.();
                onCreatePRChange(e.target.checked);
              }}
            />
            <span>Create pull request</span>
          </label>
          {forcedCreatePR && <ForcedBadge />}
        </div>
        <span className="bf-hint">
          {creating ? (
            <span className="italic" style={{ color: 'var(--grey0)' }}>opens after approved review</span>
          ) : prDisplay ? (
            <a
              href={prDisplay.href}
              target="_blank"
              rel="noopener noreferrer"
              style={{ color: 'var(--aqua)', fontFamily: 'var(--font-mono)' }}
            >
              {prDisplay.label} ↗
            </a>
          ) : (
            <span className="italic" style={{ color: 'var(--grey0)' }}>none</span>
          )}
        </span>
      </div>

      {/* Base branch */}
      <div className="bf-spread">
        <span className="bf-switch-label">Base branch</span>
        <select
          aria-label="Base branch"
          value={baseBranch ?? ''}
          onChange={(e) => onBaseBranchChange(e.target.value)}
          disabled={branchesLoading || disabled}
          className="bf-input"
          style={{ width: 'auto', minWidth: '160px' }}
        >
          <option value="">Default branch</option>
          {branches.map((b) => (
            <option key={b} value={b}>{b}</option>
          ))}
        </select>
      </div>
      {branchesError && (
        <div className="text-xs text-[var(--yellow)] -mt-1">Could not load branches</div>
      )}

      {/* Review attempts (subtle, only when relevant) */}
      {!creating && reviewAttempts != null && reviewAttempts > 0 && (
        <div
          className="font-mono"
          style={{
            color: reviewAttempts >= MAX_REVIEW_ATTEMPTS ? 'var(--yellow)' : 'var(--grey1)',
            fontSize: '11.5px',
          }}
        >
          {reviewAttempts} review attempt{reviewAttempts === 1 ? '' : 's'} · max {MAX_REVIEW_ATTEMPTS}
        </div>
      )}

      {/* Locked banner — shown whenever the inputs are disabled outside
          create mode. The banner text is supplied by the caller so the
          message can describe the actual reason (runner attached vs.
          state-not-todo). */}
      {disabled && !creating && (
        <div className="bf-locked-banner">
          🔒 {lockedReason ?? 'Automation locked during remote run'}
        </div>
      )}
    </div>
  );
}

function ForcedBadge() {
  return (
    <span
      className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full font-mono"
      style={{
        background: 'color-mix(in oklab, var(--bg-yellow) 70%, transparent)',
        color: 'var(--yellow)',
        border: '1px solid color-mix(in oklab, var(--yellow) 30%, transparent)',
        fontSize: '9.5px',
        letterSpacing: '0.04em',
        marginLeft: '24px',
        alignSelf: 'flex-start',
        whiteSpace: 'nowrap',
      }}
      title="The server force-enabled this flag for the current Run"
    >
      <span aria-hidden="true">⚡</span>forced on run
    </span>
  );
}

/**
 * Parses a PR URL into a short display label and an `href`. Recognised
 * shapes: GitHub (`/pull/N`), GitLab (`/merge_requests/N`), Gitea (`/pulls/N`).
 * Falls back to the URL's last path segment.
 */
function formatPrLink(prUrl: string | undefined): { label: string; href: string } | null {
  if (!prUrl || !isSafeHttpUrl(prUrl)) return null;
  const m = prUrl.match(/\/(?:pull|pulls|pr|merge_requests)\/(\d+)\b/);
  if (m) return { label: `PR #${m[1]}`, href: prUrl };
  const seg = prUrl.split('/').filter(Boolean).pop();
  return { label: seg ? `PR ${seg}` : 'PR', href: prUrl };
}
