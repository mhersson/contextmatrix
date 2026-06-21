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
 * Per-card model pins for the agent backend — three rows (Orchestrator /
 * Coder / Reviewer) in the automation rail's `.bf-spread` row style. Each row
 * is a free-text input bound to a pin value, with OpenRouter-catalog
 * autocomplete via a shared `<datalist>`. An empty pin means the agent's
 * complexity selector decides the model, surfaced by the right-aligned hint.
 *
 * When `favorites` is supplied, a chip row above the inputs lets operators
 * click a preferred slug into the first empty pin.
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
  // (ProjectShell renders both independently), so the datalist id must be
  // instance-unique — a hardcoded id would be duplicated and one panel
  // would silently lose autocomplete.
  const listId = useId();
  const values: Record<ModelPinField, string> = {
    model_orchestrator: orchestrator,
    model_coder: coder,
    model_reviewer: reviewer,
  };

  /** Click handler: fill the first empty pin, falling back to orchestrator. */
  function handleFavoriteClick(slug: string) {
    const firstEmpty = ROWS.find(({ field }) => !values[field]);
    onChange(firstEmpty?.field ?? 'model_orchestrator', slug);
  }

  const showFavorites = favorites && favorites.length > 0;

  return (
    <>
      {showFavorites && (
        <div
          className="bf-spread"
          style={{ flexWrap: 'wrap', alignItems: 'flex-start', gap: '4px 6px' }}
        >
          <span
            className="bf-switch-label"
            style={{ flexShrink: 0, alignSelf: 'center' }}
          >
            Favorites
          </span>
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: '4px',
              minWidth: 0,
              alignItems: 'center',
            }}
          >
            {favorites.map((slug) => (
              <button
                key={slug}
                type="button"
                disabled={disabled}
                onClick={() => handleFavoriteClick(slug)}
                title={`Set favorite: ${slug}`}
                style={{
                  background: 'color-mix(in oklab, var(--bg-blue) 70%, transparent)',
                  color: 'var(--aqua)',
                  border: '1px solid color-mix(in oklab, var(--aqua) 30%, transparent)',
                  borderRadius: '4px',
                  padding: '1px 7px',
                  fontFamily: 'var(--font-mono)',
                  fontSize: '10.5px',
                  cursor: disabled ? 'default' : 'pointer',
                  whiteSpace: 'nowrap',
                  lineHeight: '1.6',
                  opacity: disabled ? 0.5 : 1,
                }}
              >
                {slug}
              </button>
            ))}
          </div>
        </div>
      )}
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
