import { useEffect, useId, useMemo, useState } from 'react';
import { api } from '../../api/client';
import type { TaskSkillSummary } from '../../types';

type Mode = 'inherit' | 'specific' | 'none';

interface Props {
  value: string[] | null | undefined;
  onChange: (next: string[] | null) => void;
}

// modeFor maps the three-state value into the radio mode currently shown.
function modeFor(value: string[] | null | undefined): Mode {
  if (value === null || value === undefined) return 'inherit';
  if (value.length === 0) return 'none';
  return 'specific';
}

export function DefaultSkillsSelector({ value, onChange }: Props) {
  const [skills, setSkills] = useState<TaskSkillSummary[] | null>(null);
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
      .then(s => { if (!cancelled) { setSkills(s); setLoading(false); } })
      .catch(err => {
        if (cancelled) return;
        setError(err?.error ?? 'Failed to load task skills');
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  // Currently-selected names (only populated in 'specific' mode).
  const selected = useMemo(() => new Set(value ?? []), [value]);

  const setMode = (next: Mode) => {
    setLocalMode(next);
    if (next === 'inherit') onChange(null);
    else if (next === 'none') onChange([]);
    // 'specific': do NOT call onChange — checkboxes become visible via localMode
  };

  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name); else next.add(name);
    onChange(Array.from(next).sort());
  };

  return (
    <div>
      <div id={headingId} className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
        Default task skills
      </div>
      <div className="p-3 rounded space-y-3" style={{ backgroundColor: 'var(--bg1)' }} aria-labelledby={headingId}>
        <ModeRadio
          name={radioName}
          mode={localMode}
          value="inherit"
          label="Mount the full task-skills set (default)"
          onChange={setMode}
        />
        <ModeRadio
          name={radioName}
          mode={localMode}
          value="specific"
          label="Constrain to selected skills"
          onChange={setMode}
        />
        {localMode === 'specific' && (
          <div className="pl-6">
            {loading && <div className="text-xs" style={{ color: 'var(--grey1)' }}>Loading…</div>}
            {error && <div className="text-xs" style={{ color: 'var(--red)' }}>{error}</div>}
            {!loading && !error && skills && skills.length === 0 && (
              <div className="text-xs" style={{ color: 'var(--grey1)' }}>
                No task skills available. Configure <code>task_skills.dir</code> on the server.
              </div>
            )}
            {!loading && !error && skills && skills.length > 0 && (
              <div className="space-y-1.5 max-h-64 overflow-y-auto pr-2">
                {skills.map(s => (
                  <label key={s.name} className="flex items-start gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={selected.has(s.name)}
                      onChange={() => toggle(s.name)}
                      className="mt-0.5 accent-[var(--green)]"
                    />
                    <span className="text-sm leading-tight" style={{ color: 'var(--fg)' }}>
                      <span className="font-mono">{s.name}</span>
                      <span className="block text-xs" style={{ color: 'var(--grey1)' }}>{s.description}</span>
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
        />
      </div>
    </div>
  );
}

interface ModeRadioProps {
  name: string;
  mode: Mode;
  value: Mode;
  label: string;
  onChange: (next: Mode) => void;
}

function ModeRadio({ name, mode, value, label, onChange }: ModeRadioProps) {
  return (
    <label className="flex items-center gap-2 cursor-pointer">
      <input
        type="radio"
        name={name}
        checked={mode === value}
        onChange={() => onChange(value)}
        className="accent-[var(--green)]"
      />
      <span className="text-sm" style={{ color: 'var(--fg)' }}>{label}</span>
    </label>
  );
}
