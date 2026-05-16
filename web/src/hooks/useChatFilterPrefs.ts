import { useCallback, useState } from 'react';

const STORAGE_KEY = 'chat_filter_prefs';

export interface ChatFilterPrefs {
  showText: boolean;
  showToolCalls: boolean;
  showThinking: boolean;
}

const DEFAULTS: ChatFilterPrefs = {
  showText: true,
  showToolCalls: false,
  showThinking: false,
};

function loadFromStorage(): ChatFilterPrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULTS;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return DEFAULTS;
    const p = parsed as Record<string, unknown>;
    return {
      showText: typeof p.showText === 'boolean' ? p.showText : DEFAULTS.showText,
      showToolCalls: typeof p.showToolCalls === 'boolean' ? p.showToolCalls : DEFAULTS.showToolCalls,
      showThinking: typeof p.showThinking === 'boolean' ? p.showThinking : DEFAULTS.showThinking,
    };
  } catch {
    return DEFAULTS;
  }
}

function saveToStorage(prefs: ChatFilterPrefs): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch (err) {
    // Quota exceeded or storage blocked (private mode). Surface once so a dev
    // notices regressions; do not block the UI — persistence is best-effort.
    console.warn('chat_filter_prefs: persist failed', err);
  }
}

export interface UseChatFilterPrefsResult {
  prefs: ChatFilterPrefs;
  setPref: <K extends keyof ChatFilterPrefs>(key: K, value: ChatFilterPrefs[K]) => void;
}

export function useChatFilterPrefs(): UseChatFilterPrefsResult {
  const [prefs, setPrefs] = useState<ChatFilterPrefs>(loadFromStorage);

  const setPref = useCallback(<K extends keyof ChatFilterPrefs>(key: K, value: ChatFilterPrefs[K]) => {
    setPrefs((prev) => {
      const next = { ...prev, [key]: value };
      saveToStorage(next);
      return next;
    });
  }, []);

  return { prefs, setPref };
}
