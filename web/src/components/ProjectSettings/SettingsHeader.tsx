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

/**
 * Board-band style header for the settings page: display name as the
 * Fraunces title, the read-only identity (prefix, card count, boards repo)
 * in the sub-line, and the Save / Discard cluster on the right - the same
 * spot the card panel keeps Save. Read-only viewers see the lock note
 * instead of buttons.
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
  return (
    <header className="ps-head">
      <div className="ps-head__main">
        <h2 className="ps-head__title">{config.display_name ?? config.name}</h2>
        <div className="ps-head__sub">
          <span className="ps-prefix">{config.prefix}</span>
          <span className="sep" aria-hidden="true">·</span>
          <span>{`${cardCount} ${cardCount === 1 ? 'card' : 'cards'}`}</span>
          {boardsRepo && (
            <>
              <span className="sep" aria-hidden="true">·</span>
              <span>
                boards <span className="mono">{boardsRepo}</span>
              </span>
            </>
          )}
        </div>
      </div>
      <div className="ps-head__actions">
        {readOnly ? (
          <span className="ps-readonly">🔒 Read-only · only admins change project settings</span>
        ) : (
          <>
            {isDirty && (
              <>
                <span className="ps-unsaved">unsaved changes</span>
                <button type="button" onClick={onDiscard} disabled={isSaving} className="bf-btn-ghost">
                  Discard
                </button>
              </>
            )}
            <button
              type="button"
              onClick={onSave}
              disabled={!isDirty || isSaving}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
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
