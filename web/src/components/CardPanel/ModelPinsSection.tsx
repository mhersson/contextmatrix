import { useId } from 'react';

export type ModelPinField = 'model_orchestrator' | 'model_coder' | 'model_reviewer';

interface ModelPinsSectionProps {
  orchestrator: string;
  coder: string;
  reviewer: string;
  onChange: (field: ModelPinField, value: string) => void;
  disabled?: boolean;
  /** OpenRouter catalog slugs for autocomplete; [] = free-text only. */
  models: string[];
}

const ROWS: { field: ModelPinField; label: string }[] = [
  { field: 'model_orchestrator', label: 'Orchestrator' },
  { field: 'model_coder', label: 'Coder' },
  { field: 'model_reviewer', label: 'Reviewer' },
];

/**
 * Per-card model pins for the agent backend — three rows (Orchestrator /
 * Coder / Reviewer) in the automation rail's `.bf-spread` row style. Each row
 * is a free-text input bound to a pin value, with OpenRouter-catalog
 * autocomplete via a shared `<datalist>`. An empty pin means the agent's
 * complexity selector decides the model, surfaced by the right-aligned hint.
 *
 * The locked-state banner is owned by the parent (AutomationCheckboxes); this
 * component only honors `disabled`.
 */
export function ModelPinsSection({
  orchestrator,
  coder,
  reviewer,
  onChange,
  disabled = false,
  models,
}: ModelPinsSectionProps) {
  // CardPanel and CreateCardPanel can be mounted simultaneously
  // (ProjectShell renders both independently), so the datalist id must be
  // instance-unique — a hardcoded id would be duplicated and one panel
  // would silently lose autocomplete.
  const listId = useId();
  const values: Record<ModelPinField, string> = {
    model_orchestrator: orchestrator,
    model_coder: coder,
    model_reviewer: reviewer,
  };

  return (
    <>
      {ROWS.map(({ field, label }) => {
        const value = values[field];
        const inputId = `${listId}-${field}`;
        return (
          <div className="bf-spread" key={field}>
            <label
              className="bf-switch-label"
              htmlFor={inputId}
              style={{ flexShrink: 0 }}
            >
              {label}
            </label>
            <div className="flex items-center gap-2 min-w-0">
              <input
                id={inputId}
                type="text"
                list={listId}
                aria-label={`${label} model pin`}
                value={value}
                disabled={disabled}
                placeholder="selector decides"
                onChange={(e) => onChange(field, e.target.value)}
                className="bf-input font-mono"
                style={{ width: 'auto', minWidth: '180px', fontSize: '11.5px' }}
              />
            </div>
          </div>
        );
      })}
      <datalist id={listId}>
        {models.map((slug) => (
          <option key={slug} value={slug} />
        ))}
      </datalist>
    </>
  );
}
