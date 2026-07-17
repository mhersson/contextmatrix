import { isSafeHttpUrl } from './utils';
import { ModelPinsSection, type ModelPinField } from './ModelPinsSection';

const REVIEW_ATTEMPTS_WARN_THRESHOLD = 4;
const REVIEW_ATTEMPTS_HALT = 5;
const MOB_PHASES = ['plan', 'review', 'execute'] as const;

interface AutomationCheckboxesProps {
  autonomous: boolean;
  featureBranch: boolean;
  createPR: boolean;
  onAutonomousChange: (value: boolean) => void;
  onFeatureBranchChange: (value: boolean) => void;
  onCreatePRChange: (value: boolean) => void;
  /**
   * Active task backend ("agent" | ""). When `'agent'`, the three model-pin
   * inputs render as the model-steering row. When unset there is no
   * steering row at all.
   */
  taskBackend?: string;
  /** Model pin values - only rendered when `taskBackend === 'agent'`. */
  modelOrchestrator?: string;
  modelCoder?: string;
  modelReviewer?: string;
  /**
   * Pin-change handler. Required even though it is only invoked on the
   * agent path: it is effectively mandatory whenever the pins render, and
   * a required prop turns forgotten wiring into a compile error instead of
   * silently dead inputs.
   */
  onModelPinChange: (field: ModelPinField, value: string) => void;
  /** OpenRouter catalog slugs for pin autocomplete; [] = free-text only. */
  models?: string[];
  /**
   * Operator-configured favorite slugs (flattened across tiers, de-duped).
   * Passed through to ModelPinsSection which renders them as preset chips.
   * Only relevant when taskBackend === 'agent'.
   */
  favorites?: string[];
  /** Current Best-of-N candidate count. 0/undefined = off. */
  bestOfN?: number;
  /** Upper bound for the selector, from app config (`best_of_n_max`). */
  bestOfNMax?: number;
  /** Operator-recommended candidate count, surfaced in the control's tooltip. */
  bestOfNDefault?: number;
  /**
   * Best-of-N change handler. Optional only because edit-mode callers may
   * leave it unmounted when the backend is not the agent path; create mode
   * wires it through CreateCardPanel.
   */
  onBestOfNChange?: (value: number) => void;
  /** Current mob seat count. 0/undefined = off. */
  mobParticipants?: number;
  /** Upper bound for the seats selector, from app config (`mob_max_participants`). */
  mobMaxParticipants?: number;
  /** Operator-recommended seat count, surfaced in the control's tooltip. */
  mobDefaultParticipants?: number;
  /** Phases the card convenes discussions in (subset of plan/review/execute). */
  mobPhases?: string[];
  /** Guest names selected on the card. */
  mobGuests?: string[];
  /** Registry guest names from app config (`mob_guest_names`). */
  mobGuestNames?: string[];
  /**
   * Whether the server allows the mob "execute" phase
   * (`mob_execute_checkpoints`). Gates the execute pill and the Best-of-N
   * exclusion: mob coding takes priority over Best-of-N.
   */
  mobExecuteCheckpoints?: boolean;
  /**
   * Mob change handlers. Optional like `onBestOfNChange` - the block only
   * renders on the agent backend path; create mode wires them through
   * CreateCardPanel.
   */
  onMobParticipantsChange?: (value: number) => void;
  onMobPhasesChange?: (value: string[]) => void;
  onMobGuestsChange?: (value: string[]) => void;
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
   * placeholder hints - there's nothing to run yet.
   */
  mode?: 'edit' | 'create';
  /**
   * Optional override for the locked-state banner text. Defaults to the
   * worker-attached message when omitted; callers pass a state-specific
   * message (e.g. "Automation can only be edited in todo") when the lock
   * is driven by the card's lifecycle rather than an active worker.
   */
  lockedReason?: string;
}

/**
 * Automation rail - mirrors the design mock's `.spread` rows
 * (`/tmp/card-panel-explorer.html:2003-2048`). Each row puts the control
 * label on the left and a hint/value on the right:
 *
 *   [☐ Autonomous mode]            HITL - human replies in chat
 *   [☐ Feature branch]            ctxmax-456/foo
 *   [☐ Create pull request]       PR #482 ↗
 *   Base branch                   [main ▾]
 *   1 review attempt · max 5
 *   🔒 Automation locked during remote run        (when disabled)
 *
 * Run-status info lives inline with each row - no separate "Run status"
 * card. The autonomous hint is uncolored (just `.bf-hint` defaults).
 */
export function AutomationCheckboxes({
  autonomous, featureBranch, createPR,
  onAutonomousChange, onFeatureBranchChange, onCreatePRChange,
  taskBackend,
  modelOrchestrator = '', modelCoder = '', modelReviewer = '',
  onModelPinChange, models = [], favorites,
  bestOfN, bestOfNMax, bestOfNDefault, onBestOfNChange,
  mobParticipants, mobMaxParticipants, mobDefaultParticipants,
  mobPhases, mobGuests, mobGuestNames, mobExecuteCheckpoints,
  onMobParticipantsChange, onMobPhasesChange, onMobGuestsChange,
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
  const agentBackend = taskBackend === 'agent';
  const bestOfNMaxResolved = bestOfNMax ?? 5;
  const bestOfNDefaultResolved = bestOfNDefault ?? 3;
  const bestOfNOptions = Array.from(
    { length: Math.max(bestOfNMaxResolved - 1, 0) },
    (_, i) => i + 2,
  );
  const mobMaxResolved = mobMaxParticipants ?? 5;
  const mobDefaultResolved = mobDefaultParticipants ?? 3;
  const mobOptions = Array.from(
    { length: Math.max(mobMaxResolved - 1, 0) },
    (_, i) => i + 2,
  );
  const mobOn = (mobParticipants ?? 0) >= 2;
  const executeSelected = mobOn && (mobPhases ?? []).includes('execute');
  // Best-of-N is excluded only when the execute phase will actually run:
  // with the server flag off the trigger drops execute and Best-of-N runs.
  const exclusionActive = executeSelected && (mobExecuteCheckpoints ?? false);

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

      {/* Best of N - agent backend only. Renders in both edit and create
          modes when the agent backend is active. */}
      {agentBackend && (
        <>
          <div className="bf-spread">
            <span className={`bf-switch-label ${exclusionActive ? 'opacity-50' : ''}`}>Best of N</span>
            <select
              aria-label="Best of N"
              value={bestOfN ?? 0}
              onChange={(e) => onBestOfNChange?.(Number(e.target.value))}
              disabled={disabled || exclusionActive}
              className="bf-input"
              style={{ width: 'auto', minWidth: '160px' }}
              title={exclusionActive
                ? 'Disabled - mob coding takes priority'
                : `Off, or 2–${bestOfNMaxResolved} candidate models judged per run (default ${bestOfNDefaultResolved})`}
            >
              <option value={0}>Off</option>
              {bestOfNOptions.map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </select>
          </div>
          {exclusionActive && (bestOfN ?? 0) >= 2 && (
            <div className="text-xs -mt-1" style={{ color: 'var(--yellow)' }}>
              Mob coding takes priority - Best of N is ignored while the mob execute phase is selected.
            </div>
          )}
        </>
      )}

      {/* Mob discussions - agent backend only. Renders in both edit and
          create modes (mirrors the Best-of-N rule). */}
      {agentBackend && (
        <>
          <div className="bf-spread">
            <span className="bf-switch-label">Mob seats</span>
            <select
              aria-label="Mob seats"
              value={mobParticipants ?? 0}
              onChange={(e) => {
                const n = Number(e.target.value);
                onMobParticipantsChange?.(n);
                if (n >= 2 && !mobOn) {
                  // Enabling from Off: default the phase set.
                  onMobPhasesChange?.(['plan', 'review']);
                } else if (n === 0) {
                  // Turning Off: clear the dependent fields so a stale
                  // guest list can't fail validation later.
                  onMobPhasesChange?.([]);
                  onMobGuestsChange?.([]);
                }
              }}
              disabled={disabled}
              className="bf-input"
              style={{ width: 'auto', minWidth: '160px' }}
              title={`Off, or 2–${mobMaxResolved} agents discussing plan/review/execute (default ${mobDefaultResolved})`}
            >
              <option value={0}>Off</option>
              {mobOptions.map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </select>
          </div>

          {mobOn && (
            <div className="bf-spread">
              <span className="bf-switch-label">Mob phases</span>
              <div className="flex items-center gap-2">
                {MOB_PHASES.map((phase) => {
                  const active = (mobPhases ?? []).includes(phase);
                  const serverOff = phase === 'execute' && !(mobExecuteCheckpoints ?? false);
                  return (
                    <button
                      key={phase}
                      type="button"
                      className="chip-pill"
                      aria-pressed={active}
                      aria-label={`Mob phase ${phase}`}
                      disabled={disabled || serverOff}
                      title={serverOff ? 'Execute checkpoints are disabled on this server' : undefined}
                      style={{
                        backgroundColor: active ? 'var(--bg-purple)' : 'var(--bg2)',
                        color: active ? 'var(--purple)' : 'var(--grey1)',
                        cursor: disabled || serverOff ? 'default' : 'pointer',
                        opacity: serverOff ? 0.5 : undefined,
                      }}
                      onClick={() => {
                        const current = mobPhases ?? [];
                        const next = active
                          ? current.filter((p) => p !== phase)
                          : [...current, phase];
                        onMobPhasesChange?.(next);
                      }}
                    >
                      {phase}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {mobOn && (mobGuestNames?.length ?? 0) > 0 && (
            <div className="bf-spread">
              <span className="bf-switch-label">Mob guests</span>
              <div className="flex items-center gap-2 flex-wrap">
                {mobGuestNames!.map((name) => {
                  const active = (mobGuests ?? []).includes(name);
                  return (
                    <button
                      key={name}
                      type="button"
                      className="chip-pill"
                      aria-pressed={active}
                      aria-label={`Mob guest ${name}`}
                      disabled={disabled}
                      style={{
                        backgroundColor: active ? 'var(--bg-blue)' : 'var(--bg2)',
                        color: active ? 'var(--aqua)' : 'var(--grey1)',
                        cursor: disabled ? 'default' : 'pointer',
                      }}
                      onClick={() => {
                        const current = mobGuests ?? [];
                        const next = active
                          ? current.filter((g) => g !== name)
                          : [...current, name];
                        onMobGuestsChange?.(next);
                      }}
                    >
                      {name}
                    </button>
                  );
                })}
              </div>
            </div>
          )}
        </>
      )}

      {/* Orchestrator steering wheel - the agent backend uses per-role model
          pins. No steering row on the unset backend. */}
      {agentBackend && (
        <ModelPinsSection
          orchestrator={modelOrchestrator}
          coder={modelCoder}
          reviewer={modelReviewer}
          onChange={onModelPinChange}
          disabled={disabled}
          models={models}
          favorites={favorites}
        />
      )}

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
            color: reviewAttempts >= REVIEW_ATTEMPTS_WARN_THRESHOLD ? 'var(--yellow)' : 'var(--grey1)',
            fontSize: '11.5px',
          }}
        >
          {reviewAttempts} review attempt{reviewAttempts === 1 ? '' : 's'} · max {REVIEW_ATTEMPTS_HALT}
        </div>
      )}

      {/* Locked banner - shown whenever the inputs are disabled outside
          create mode. The banner text is supplied by the caller so the
          message can describe the actual reason (worker attached vs.
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
