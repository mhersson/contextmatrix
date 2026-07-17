/**
 * localStorage helpers that fail closed in private-browsing mode and on
 * malformed values. Use these instead of touching `localStorage` directly
 * when persisting a single boolean preference (e.g. a rail's expanded state).
 *
 * Both helpers return/accept booleans only. Anything else stored at the key
 * (legacy values, hand-edited entries) is treated as "unset" by the reader.
 */

export function safeReadBool(key: string): boolean | undefined {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) return undefined;
    if (raw === 'true') return true;
    if (raw === 'false') return false;
    return undefined;
  } catch {
    return undefined;
  }
}

export function safeWriteBool(key: string, value: boolean): void {
  try {
    localStorage.setItem(key, String(value));
  } catch {
    // ignore - private mode, quota exceeded, etc.
  }
}
