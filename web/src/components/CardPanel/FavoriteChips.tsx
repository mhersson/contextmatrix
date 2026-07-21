interface FavoriteChipsProps {
  /** Operator-configured favorite slugs (flattened across tiers, de-duped). */
  favorites: string[];
  disabled: boolean;
  onPick: (slug: string) => void;
}

/**
 * The "Favorites" chip row above the model pins - one clickable chip per
 * operator-configured favorite slug. Which pin a click fills is the parent's
 * decision (ModelPinsSection targets the first empty pin).
 */
export function FavoriteChips({ favorites, disabled, onPick }: FavoriteChipsProps) {
  return (
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
            onClick={() => onPick(slug)}
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
  );
}
