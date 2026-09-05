import type { ProjectConfig } from '../../types';

interface SettingsHeaderProps {
  config: ProjectConfig;
  cardCount: number;
  /** Set only on multi-repo instances, where the repo name carries information. */
  boardsRepo?: string;
  readOnly: boolean;
  isDirty: boolean;
  isSaving: boolean;
  onSave: () => void;
  onDiscard: () => void;
}

// Save keeps the card panel's exact button; Discard shares its box so the
// pair reads as one cluster.
const buttonBox = 'px-3 py-1.5 rounded text-sm font-medium transition-colors';

/**
 * Board-band style header for the settings page: display name as the
 * Fraunces title, the read-only identity (prefix, slug when it differs,
 * card count, boards repo) in the sub-line, and the Save / Discard cluster
 * on the right - the same spot the card panel keeps Save. Read-only viewers
 * see the lock note instead of buttons.
 */
export function SettingsHeader({
  config,
  cardCount,
  boardsRepo,
  readOnly,
  isDirty,
  isSaving,
  onSave,
  onDiscard,
}: SettingsHeaderProps) {
  const title = config.display_name ?? config.name;
  return (
    <header className="ps-head">
      <div className="ps-head__main">
        <h2 className="ps-head__title">{title}</h2>
        <div className="ps-head__sub">
          <span className="ps-prefix">{config.prefix}</span>
          {title !== config.name && (
            <>
              <span className="sep" aria-hidden="true">·</span>
              <span className="mono">{config.name}</span>
            </>
          )}
          <span className="sep" aria-hidden="true">·</span>
          <span>{`${cardCount} ${cardCount === 1 ? 'card' : 'cards'}`}</span>
          {boardsRepo && (
            <>
              <span className="sep" aria-hidden="true">·</span>
              <span title="Boards repository">
                <span className="sr-only">Boards repository </span>
                <span className="mono">{boardsRepo}</span>
              </span>
            </>
          )}
        </div>
      </div>
      <div className="ps-head__actions">
        {readOnly ? (
          <span className="ps-readonly">
            <span className="ps-readonly__glyph" aria-hidden="true">🔒 </span>
            Read-only · only admins change project settings
          </span>
        ) : (
          <>
            {isDirty && (
              <>
                <span className="ps-unsaved">
                  <span className="ps-tab-dot" aria-hidden="true" />
                  unsaved changes
                </span>
                <button
                  type="button"
                  onClick={onDiscard}
                  disabled={isSaving}
                  className={`${buttonBox} border border-[var(--bg4)] text-[var(--fg)] hover:bg-[var(--bg2)] disabled:opacity-50`}
                >
                  Discard
                </button>
              </>
            )}
            <button
              type="button"
              onClick={onSave}
              disabled={!isDirty || isSaving}
              className={`${buttonBox} ${
                isDirty && !isSaving
                  ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
                  : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
              }`}
            >
              {isSaving ? 'Saving…' : 'Save changes'}
            </button>
          </>
        )}
      </div>
    </header>
  );
}
