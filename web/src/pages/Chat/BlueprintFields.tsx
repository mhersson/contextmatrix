import { useId } from 'react';
import { Field } from './Field';
import { ProjectList, type ProjectOption } from './ProjectList';
import {
  BLUEPRINT_MODES,
  CAP_PRESETS,
  capLabel,
  capSentence,
  type BlueprintForm,
  type BlueprintMode,
} from './blueprintForm';

interface BlueprintFieldsProps {
  projects: ProjectOption[];
  value: BlueprintForm;
  onChange: (next: BlueprintForm) => void;
}

export function BlueprintFields({ projects, value, onChange }: BlueprintFieldsProps) {
  const picked = value.projects.length;
  return (
    <>
      <Field label="Projects" hint={picked > 0 ? `${picked} selected` : 'Pick at least one'}>
        <ProjectList
          projects={projects}
          multi
          selected={value.projects}
          onChange={(next) => onChange({ ...value, projects: next })}
        />
      </Field>
      <Field label="Mode">
        <ModeCards mode={value.mode} onChange={(mode) => onChange({ ...value, mode })} />
      </Field>
      <Field label="Cost cap">
        <CapPresets cap={value.capUSD} onChange={(capUSD) => onChange({ ...value, capUSD })} />
      </Field>
    </>
  );
}

function ModeCards({ mode, onChange }: { mode: BlueprintMode; onChange: (mode: BlueprintMode) => void }) {
  const group = useId();
  return (
    <div className="nd-modes" role="radiogroup" aria-label="Mode">
      {BLUEPRINT_MODES.map((m) => {
        const on = m.value === mode;
        const descId = `${group}-${m.value}`;
        return (
          <label
            key={m.value}
            className={`nd-mode${on ? ' nd-mode--on' : ''}${m.value === 'yolo' ? ' nd-mode--yolo' : ''}`}
          >
            <input
              type="radio"
              className="sr-only"
              name={group}
              checked={on}
              aria-label={m.label}
              aria-describedby={descId}
              onChange={() => onChange(m.value)}
            />
            <b>{m.label}</b>
            <span id={descId}>{m.description}</span>
          </label>
        );
      })}
    </div>
  );
}

function CapPresets({ cap, onChange }: { cap: number; onChange: (cap: number) => void }) {
  const group = useId();
  return (
    <>
      <div className="nd-caps" role="radiogroup" aria-label="Cost cap">
        {CAP_PRESETS.map((value) => (
          <label key={value} className={`nd-cap${value === cap ? ' nd-cap--on' : ''}`}>
            <input
              type="radio"
              className="sr-only"
              name={group}
              checked={value === cap}
              onChange={() => onChange(value)}
            />
            {capLabel(value)}
          </label>
        ))}
      </div>
      <p className="nd-note">{capSentence(cap)}</p>
    </>
  );
}
