import { settingsPanelId, settingsTabId, type SettingsTabKey } from './settingsTabIds';

export type { SettingsTabKey } from './settingsTabIds';

export interface SettingsTab {
  key: SettingsTabKey;
  label: string;
  danger?: boolean;
  /** The tab's sections hold edits that have not been saved. */
  dirty: boolean;
}

interface SettingsTabsProps {
  tabs: SettingsTab[];
  active: SettingsTabKey;
  onChange: (key: SettingsTabKey) => void;
}

/** The card panel's rail tab strip, pinned to the top of the settings
 *  scroller. A dirty tab carries a yellow dot plus screen-reader text. */
export function SettingsTabs({ tabs, active, onChange }: SettingsTabsProps) {
  return (
    <div className="ps-tabs" role="tablist" aria-label="Project settings tabs">
      {tabs.map((t) => {
        const isActive = t.key === active;
        return (
          <button
            key={t.key}
            id={settingsTabId(t.key)}
            type="button"
            role="tab"
            aria-selected={isActive}
            aria-controls={settingsPanelId(t.key)}
            onClick={() => onChange(t.key)}
            className={`bf-rail-tab${isActive ? ' bf-rail-tab--active' : ''}${t.danger ? ' bf-rail-tab--danger' : ''}`}
          >
            {t.danger && <span aria-hidden="true">⚠</span>}
            <span>{t.label}</span>
            {t.dirty && (
              <>
                <span className="ps-tab-dot" aria-hidden="true" />
                <span className="sr-only"> (unsaved changes)</span>
              </>
            )}
          </button>
        );
      })}
    </div>
  );
}
