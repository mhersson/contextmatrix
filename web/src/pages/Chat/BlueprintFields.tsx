import { useId } from 'react';
import { ChatModelPicker } from './ChatModelPicker';
import { Field } from './Field';
import { ProjectList, type ProjectOption } from './ProjectList';
import type { ChatModelChoice } from './useChatModelChoice';
import {
  BLUEPRINT_MODES,
  CAP_PRESETS,
  PLAIN_LANGUAGE,
  UNATTENDED,
  capLabel,
  capSentence,
  type BlueprintForm,
  type BlueprintMode,
  withMode,
  withUnattended,
} from './blueprintForm';

interface BlueprintFieldsProps {
  projects: ProjectOption[];
  value: BlueprintForm;
  onChange: (next: BlueprintForm) => void;
  models: ChatModelChoice;
  /** Projects a hand-off named that CM no longer has. */
  leftOut?: readonly string[];
}

export function BlueprintFields({ projects, value, onChange, models, leftOut = [] }: BlueprintFieldsProps) {
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
        {leftOut.length > 0 && <p className="nd-note">Left out, no longer in ContextMatrix: {leftOut.join(', ')}</p>}
      </Field>
      <Field label="Mode">
        <ModeCards mode={value.mode} onChange={(mode) => onChange(withMode(value, mode))} />
      </Field>
      <FlagSwitch
        {...PLAIN_LANGUAGE}
        on={value.plainLanguage}
        onChange={(plainLanguage) => onChange({ ...value, plainLanguage })}
      />
      <FlagSwitch {...UNATTENDED} on={value.unattended} onChange={(on) => onChange(withUnattended(value, on))} />
      <ChatModelPicker {...models} />
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

function FlagSwitch({
  label,
  description,
  on,
  onChange,
}: {
  label: string;
  description: string;
  on: boolean;
  onChange: (on: boolean) => void;
}) {
  const descId = useId();
  return (
    <label className={`nd-plain${on ? ' nd-plain--on' : ''}`}>
      <input
        type="checkbox"
        role="switch"
        className="sr-only"
        checked={on}
        aria-label={label}
        aria-describedby={descId}
        onChange={(e) => onChange(e.target.checked)}
      />
      <b>{label}</b>
      <span id={descId}>{description}</span>
    </label>
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
