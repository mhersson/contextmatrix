import { useId, useState } from 'react';
import { ModelCombobox } from '../ModelCombobox';
import { FavoriteChips } from './FavoriteChips';

export type ModelPinField = 'model_orchestrator' | 'model_coder' | 'model_reviewer';

interface ModelPinsSectionProps {
  orchestrator: string;
  coder: string;
  reviewer: string;
  onChange: (field: ModelPinField, value: string) => void;
  disabled?: boolean;
  /** CM model-catalog slugs; [] = catalog unavailable → free-text fallback. */
  models: string[];
  /**
   * Operator-configured favorite slugs (flattened across tiers, de-duped).
   * When present and non-empty, a chip row is rendered above the pin inputs.
   * Clicking a chip fills the first empty pin (orchestrator → coder →
   * reviewer), or the orchestrator pin when all three are already set.
   */
  favorites?: string[];
}

const ROWS: { field: ModelPinField; label: string }[] = [
  { field: 'model_orchestrator', label: 'Orchestrator' },
  { field: 'model_coder', label: 'Coder' },
  { field: 'model_reviewer', label: 'Reviewer' },
];

/**
 * Per-card model pins for the agent backend - an "Automatic model selection"
 * toggle followed by three rows (Orchestrator / Coder / Reviewer) in the
 * automation rail's `.bf-spread` row style. The toggle is checked by default
 * and hides the pin rows entirely; unchecking it reveals them. Any pin
 * already set forces the toggle off so existing pins are never hidden, and
 * re-checking it clears every set pin - hiding a pin that would still be
 * saved would make the label lie.
 *
 * Each row is a strict `ModelCombobox` bound to a pin value: typing filters
 * CM's served catalog, and only a listed slug (or empty) can be committed.
 * When `models` is `[]` (catalog unavailable) each row degrades to a plain
 * free-text input. An empty pin means the agent's complexity selector
 * decides the model, surfaced by the right-aligned hint.
 *
 * When `favorites` is supplied, a chip row above the inputs lets operators
 * click a preferred slug into the first empty pin. It hides with the pins.
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
  favorites,
}: ModelPinsSectionProps) {
  // CardPanel and CreateCardPanel can be mounted simultaneously
  // (ProjectShell renders both independently), so each row's input id must be
  // instance-unique - a hardcoded id would be duplicated and one panel's
  // label association would break.
  const listId = useId();
  const values: Record<ModelPinField, string> = {
    model_orchestrator: orchestrator,
    model_coder: coder,
    model_reviewer: reviewer,
  };

  // `revealed` only matters while all pins are empty - a set pin derives the
  // toggle off, so an external pin change (SSE refresh, discard) can never
  // strand hidden values. Per-card reset comes free from the panel's
  // key-based remount in ProjectShell.
  const [revealed, setRevealed] = useState(false);
  const hasPins = Boolean(orchestrator || coder || reviewer);
  // Latch while pins exist: without it, emptying the last pin through its
  // input would unmount the row mid-edit and re-check the toggle. Only an
  // explicit re-check (handleAutomaticToggle) drops the latch.
  if (hasPins && !revealed) setRevealed(true);
  const automatic = !hasPins && !revealed;

  function handleAutomaticToggle(checked: boolean) {
    if (checked) {
      for (const { field } of ROWS) {
        if (values[field]) onChange(field, '');
      }
    }
    setRevealed(!checked);
  }

  /** Click handler: fill the first empty pin, falling back to orchestrator. */
  function handleFavoriteClick(slug: string) {
    const firstEmpty = ROWS.find(({ field }) => !values[field]);
    onChange(firstEmpty?.field ?? 'model_orchestrator', slug);
  }


  return (
    <>
      <div className="bf-spread">
        <label className="bf-switch">
          <input
            type="checkbox"
            aria-label="Automatic model selection"
            checked={automatic}
            disabled={disabled}
            onChange={(e) => handleAutomaticToggle(e.target.checked)}
          />
          <span>Automatic model selection</span>
        </label>
        <span className="bf-hint">
          {automatic ? 'selector decides' : 'pin models per role'}
        </span>
      </div>
      {!automatic && favorites && favorites.length > 0 && (
        <FavoriteChips
          favorites={favorites}
          disabled={disabled}
          onPick={handleFavoriteClick}
        />
      )}
      {!automatic && ROWS.map(({ field, label }) => {
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
              <ModelCombobox
                id={inputId}
                value={value}
                onChange={(v) => onChange(field, v)}
                options={models}
                disabled={disabled}
                placeholder="selector decides"
                ariaLabel={`${label} model pin`}
                inputStyle={{ width: 'auto', minWidth: '180px', fontSize: '11.5px' }}
              />
            </div>
          </div>
        );
      })}
    </>
  );
}
