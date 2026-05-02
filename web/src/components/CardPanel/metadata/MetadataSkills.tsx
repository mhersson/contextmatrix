import { useEffect, useId, useMemo, useState } from 'react';
import { api } from '../../../api/client';
import type { ProjectConfig, TaskSkillSummary } from '../../../types';

type Mode = 'inherit' | 'specific' | 'none';

interface MetadataSkillsProps {
  value: string[] | null | undefined;
  config: ProjectConfig;
  onSkillsChange: (next: string[] | null) => void;
  /**
   * Lock the selector once the workflow has started — skills are mounted
   * into the runner's working directory at run start, so changes after
   * that point do not reach the live agent.
   */
  disabled?: boolean;
  /** Optional message shown next to the heading when `disabled` is true. */
  lockedReason?: string;
}

function modeFor(value: string[] | null | undefined): Mode {
  if (value === null || value === undefined) return 'inherit';
  if (value.length === 0) return 'none';
  return 'specific';
}

/**
 * Skills selector — three-state radio (inherit / specific / none) shared
 * between the card detail panel's Info tab and the create-card panel's Info
 * tab. When the project has `default_skills` set, the per-card list must be
 * a subset of the project default; other entries are hidden from the options
 * list to make the constraint visible.
 *
 * `value` is the current selection (null = inherit, [] = none, [...] =
 * specific). The parent owns the state — this component is purely
 * controlled — so it works equally well backed by `editedCard.skills` in
 * CardPanel or by local React state in CreateCardPanel.
 */
export function MetadataSkills({
  value,
  config,
  onSkillsChange,
  disabled = false,
  lockedReason,
}: MetadataSkillsProps) {
  const [allSkills, setAllSkills] = useState<TaskSkillSummary[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const headingId = useId();
  const radioName = useId();

  // Track previous value to detect external resets during render (derived-state pattern).
  const [prevValue, setPrevValue] = useState(value);
  const [localMode, setLocalMode] = useState<Mode>(modeFor(value));
  if (prevValue !== value) {
    setPrevValue(value);
    setLocalMode(modeFor(value));
  }

  useEffect(() => {
    let cancelled = false;
    api.getTaskSkills()
      .then(s => { if (!cancelled) { setAllSkills(s); setLoading(false); } })
      .catch(err => {
        if (cancelled) return;
        setError(err?.error ?? 'Failed to load task skills');
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  // Constrain the option list when the project has a default — only those
  // skills can appear on the card. Otherwise show the full available set.
  const options = useMemo(() => {
    if (!allSkills) return [];
    const projectDefault = config.default_skills;
    if (projectDefault && projectDefault.length > 0) {
      const allowed = new Set(projectDefault);
      return allSkills.filter(s => allowed.has(s.name));
    }
    return allSkills;
  }, [allSkills, config.default_skills]);

  const projectAllowsNone = config.default_skills !== undefined &&
    config.default_skills !== null &&
    config.default_skills.length === 0;

  const selected = useMemo(() => new Set(value ?? []), [value]);

  const setMode = (next: Mode) => {
    setLocalMode(next);
    if (next === 'inherit') onSkillsChange(null);
    else if (next === 'none') onSkillsChange([]);
    // 'specific': do NOT call onSkillsChange — checkboxes appear via localMode
  };

  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name); else next.add(name);
    onSkillsChange(Array.from(next).sort());
  };

  return (
    <section className={`bf-aside-section ${disabled ? 'opacity-60' : ''}`}>
      <h4 id={headingId}>Skills</h4>
      <div className="space-y-2.5">
        <ModeRadio
          name={radioName}
          mode={localMode}
          value="inherit"
          label={
            config.default_skills === null || config.default_skills === undefined
              ? 'Use project default (mount full set)'
              : projectAllowsNone
                ? 'Use project default (mount nothing)'
                : `Use project default (${(config.default_skills ?? []).length} skill${(config.default_skills ?? []).length === 1 ? '' : 's'})`
          }
          onChange={setMode}
          disabled={disabled}
        />
        <ModeRadio
          name={radioName}
          mode={localMode}
          value="specific"
          label="Specific skills"
          onChange={setMode}
          disabled={disabled || projectAllowsNone}
        />
        {localMode === 'specific' && (
          <div className="pl-6">
            {loading && <div className="text-xs" style={{ color: 'var(--grey1)' }}>Loading…</div>}
            {error && <div className="text-xs" style={{ color: 'var(--red)' }}>{error}</div>}
            {!loading && !error && options.length === 0 && (
              <div className="text-xs" style={{ color: 'var(--grey1)' }}>No skills available.</div>
            )}
            {!loading && !error && options.length > 0 && (
              <div className="space-y-1.5 max-h-48 overflow-y-auto pr-2">
                {options.map(s => (
                  <label
                    key={s.name}
                    className={`flex items-start gap-2 ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
                  >
                    <input
                      type="checkbox"
                      checked={selected.has(s.name)}
                      onChange={() => toggle(s.name)}
                      disabled={disabled}
                      className="mt-0.5 accent-[var(--green)]"
                    />
                    <span className="text-sm leading-tight" style={{ color: 'var(--fg)' }}>
                      <span className="font-mono">{s.name}</span>
                    </span>
                  </label>
                ))}
              </div>
            )}
          </div>
        )}
        <ModeRadio
          name={radioName}
          mode={localMode}
          value="none"
          label="Mount no skills"
          onChange={setMode}
          disabled={disabled}
        />
        {disabled && (
          <div className="bf-locked-banner">
            🔒 {lockedReason ?? 'Skills locked once the workflow has started'}
          </div>
        )}
      </div>
    </section>
  );
}

interface ModeRadioProps {
  name: string;
  mode: Mode;
  value: Mode;
  label: string;
  onChange: (next: Mode) => void;
  disabled?: boolean;
}

function ModeRadio({ name, mode, value, label, onChange, disabled }: ModeRadioProps) {
  return (
    <label className={`flex items-center gap-2 ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}>
      <input
        type="radio"
        name={name}
        checked={mode === value}
        onChange={() => onChange(value)}
        disabled={disabled}
        className="accent-[var(--green)]"
      />
      <span className="text-sm" style={{ color: 'var(--fg)' }}>{label}</span>
    </label>
  );
}
