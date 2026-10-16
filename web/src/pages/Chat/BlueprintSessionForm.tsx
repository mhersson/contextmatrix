import { useId } from 'react';
import { BLUEPRINT_MODES, type BlueprintDraft, type BlueprintMode } from './blueprintDraft';

interface BlueprintSessionFormProps {
  projects: { name: string; display_name?: string }[];
  value: BlueprintDraft;
  onChange: (next: BlueprintDraft) => void;
}

export function BlueprintSessionForm({ projects, value, onChange }: BlueprintSessionFormProps) {
  const modeId = useId();
  const capId = useId();

  const toggle = (name: string) => {
    const next = value.projects.includes(name)
      ? value.projects.filter((p) => p !== name)
      : [...value.projects, name];
    onChange({ ...value, projects: next });
  };

  return (
    <div className="mb-3">
      <div className="text-xs mb-1" style={{ color: 'var(--grey2)' }}>
        Target projects
      </div>
      <div className="flex flex-col gap-1 mb-3 max-h-40 overflow-y-auto">
        {projects.map((p) => (
          <label key={p.name} className="ps-check">
            <input
              type="checkbox"
              checked={value.projects.includes(p.name)}
              onChange={() => toggle(p.name)}
            />
            <span>{p.display_name ?? p.name}</span>
          </label>
        ))}
      </div>

      <label htmlFor={modeId} className="block text-xs mb-1" style={{ color: 'var(--grey2)' }}>
        Mode
      </label>
      <select
        id={modeId}
        value={value.mode}
        onChange={(e) => onChange({ ...value, mode: e.target.value as BlueprintMode })}
        className="bf-input w-full mb-3"
      >
        {BLUEPRINT_MODES.map((m) => (
          <option key={m.value} value={m.value}>
            {m.label}
          </option>
        ))}
      </select>

      <label htmlFor={capId} className="block text-xs mb-1" style={{ color: 'var(--grey2)' }}>
        Cost cap (USD) <span style={{ color: 'var(--grey1)' }}>(optional)</span>
      </label>
      <input
        id={capId}
        type="number"
        min="0"
        step="0.5"
        value={value.costCapUSD}
        onChange={(e) => onChange({ ...value, costCapUSD: e.target.value })}
        className="bf-input w-full"
        placeholder="Architect default"
      />
    </div>
  );
}
