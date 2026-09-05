export type SettingsTabKey = 'source' | 'workflow' | 'automation' | 'execution' | 'danger';

/** Shared ids so the tab strip's aria-controls and the panel's
 *  aria-labelledby point at each other. */
export function settingsPanelId(key: SettingsTabKey): string {
  return `settings-panel-${key}`;
}

export function settingsTabId(key: SettingsTabKey): string {
  return `settings-tab-${key}`;
}
