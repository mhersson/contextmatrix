import type { ResolvedCardDefaults } from '../../lib/cardDefaults';

const MOB_PHASES = ['plan', 'review', 'execute'] as const;

export interface CardDefaultsSectionProps {
  value: ResolvedCardDefaults;
  onChange: (next: ResolvedCardDefaults) => void;
  /** Active task backend; the capability and mob rows exist only for `'agent'`. */
  taskBackend: string;
  mobMaxParticipants?: number;
  mobDefaultParticipants?: number;
  mobExecuteCheckpoints?: boolean;
}

/**
 * Project-level defaults for the card Automation rail. Rows, labels and hints
 * mirror `CardPanel/AutomationCheckboxes` (create mode) so the settings read
 * as "what a new card will start with"; every accessible name is prefixed
 * "Default" so the two surfaces never share a label. Best-of-N is not
 * offered: racing candidates is a per-card decision.
 */
export function CardDefaultsSection({
  value,
  onChange,
  taskBackend,
  mobMaxParticipants,
  mobDefaultParticipants,
  mobExecuteCheckpoints,
}: CardDefaultsSectionProps) {
  const agentBackend = taskBackend === 'agent';
  const mobMax = mobMaxParticipants ?? 5;
  const mobDefault = mobDefaultParticipants ?? 3;
  const mobOptions = Array.from({ length: Math.max(mobMax - 1, 0) }, (_, i) => i + 2);
  const mobOn = value.mob_participants >= 2;

  const update = (patch: Partial<ResolvedCardDefaults>) => onChange({ ...value, ...patch });

  const handleSeats = (n: number) => {
    if (n >= 2 && !mobOn) {
      update({ mob_participants: n, mob_phases: ['review'] });
    } else if (n === 0) {
      update({ mob_participants: 0, mob_phases: [] });
    } else {
      update({ mob_participants: n });
    }
  };

  return (
    <>
      <p className="ps-lead">
        Pre-filled on every new card in this project. Each card can still override them at creation;
        subtasks always start with everything off.
      </p>

      <div className="bf-auto-stack">
          <div className="bf-spread">
            <label className="bf-switch">
              <input
                type="checkbox"
                aria-label="Default autonomous mode"
                checked={value.autonomous}
                onChange={(e) => update({ autonomous: e.target.checked })}
              />
              <span>Autonomous mode</span>
            </label>
            <span className="bf-hint">{value.autonomous ? 'no human-in-the-loop' : 'human-in-the-loop'}</span>
          </div>

          {agentBackend && (
            <div className="bf-spread">
              <label className="bf-switch">
                <input
                  type="checkbox"
                  aria-label="Default maximum capability"
                  checked={value.max_capability}
                  onChange={(e) => update({ max_capability: e.target.checked })}
                />
                <span>Maximum capability</span>
              </label>
              <span className="bf-hint">most capable in tier, ignores cost</span>
            </div>
          )}

          {agentBackend && (
            <>
              <div className="bf-spread">
                <span className="bf-switch-label">Mob seats</span>
                <select
                  aria-label="Default mob seats"
                  value={value.mob_participants}
                  onChange={(e) => handleSeats(Number(e.target.value))}
                  className="bf-state-select"
                  style={{ width: 'auto', minWidth: '160px' }}
                  title={`Off, or 2–${mobMax} agents discussing plan/review/execute (default ${mobDefault})`}
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
                      const active = value.mob_phases.includes(phase);
                      const serverOff = phase === 'execute' && !(mobExecuteCheckpoints ?? false);
                      return (
                        <button
                          key={phase}
                          type="button"
                          className="chip-pill"
                          aria-pressed={active}
                          aria-label={`Default mob phase ${phase}`}
                          disabled={serverOff}
                          title={serverOff ? 'Execute checkpoints are disabled on this server' : undefined}
                          style={{
                            backgroundColor: active ? 'var(--bg-purple)' : 'var(--bg2)',
                            color: active ? 'var(--purple)' : 'var(--grey1)',
                            cursor: serverOff ? 'default' : 'pointer',
                            opacity: serverOff ? 0.5 : undefined,
                          }}
                          onClick={() =>
                            update({
                              mob_phases: active
                                ? value.mob_phases.filter((p) => p !== phase)
                                : [...value.mob_phases, phase],
                            })
                          }
                        >
                          {phase}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </>
          )}

          <div className="bf-spread">
            <label className="bf-switch">
              <input
                type="checkbox"
                aria-label="Default create PR"
                checked={value.create_pr}
                onChange={(e) => update({ create_pr: e.target.checked })}
              />
              <span>Create pull request</span>
            </label>
            <span className="bf-hint italic" style={{ color: 'var(--grey0)' }}>opens after approved review</span>
          </div>

          {value.create_pr && (
            <>
              <div className="bf-spread" style={{ paddingLeft: '24px' }}>
                <label className="bf-switch">
                  <input
                    type="checkbox"
                    aria-label="Default wait for CI"
                    checked={value.await_ci}
                    onChange={(e) => update({ await_ci: e.target.checked })}
                  />
                  <span>Wait for CI to pass</span>
                </label>
                <span className="bf-hint">stays in review until checks pass</span>
              </div>

              <div className="bf-spread" style={{ paddingLeft: '24px' }}>
                <label className="bf-switch">
                  <input
                    type="checkbox"
                    aria-label="Default request Copilot review"
                    checked={value.await_copilot_review}
                    onChange={(e) => update({ await_copilot_review: e.target.checked })}
                  />
                  <span>Request Copilot review</span>
                </label>
                <span className="bf-hint">requests a review and addresses findings</span>
              </div>
            </>
          )}
      </div>
    </>
  );
}
