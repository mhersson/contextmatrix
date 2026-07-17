/**
 * localStorage helpers that fail closed in private-browsing mode and on
 * malformed values. Safari Private Browsing, quota exhaustion, and disabled
 * storage all throw on any localStorage access - use these instead of
 * touching `localStorage` directly so a throw never crashes a component.
 *
 * The bool variants return/accept booleans only. Anything else stored at the
 * key (legacy values, hand-edited entries) is treated as "unset" by the
 * reader.
 */

export function safeGetString(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function safeSetString(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // ignore - persistence is best-effort
  }
}

export function safeRemove(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    // ignore - persistence is best-effort
  }
}

export function safeGetJSON<T>(key: string): T | undefined {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) return undefined;
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
}

export function safeSetJSON(key: string, value: unknown): void {
  safeSetString(key, JSON.stringify(value));
}

export function safeReadBool(key: string): boolean | undefined {
  const raw = safeGetString(key);
  if (raw === 'true') return true;
  if (raw === 'false') return false;
  return undefined;
}

export function safeWriteBool(key: string, value: boolean): void {
  safeSetString(key, String(value));
}
