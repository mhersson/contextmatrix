import type { LogEntry } from '../../types';
import type { ChatFilterPrefs } from '../../hooks/useChatFilterPrefs';
import { formatHHMM, formatTitle } from '../../utils/chatTimestamp';

export type Decorated =
  | { entry: LogEntry; showStamp: false }
  | { entry: LogEntry; showStamp: true; hhmm: string; title: string };

/** Filter-bar predicate: text / tool traffic / thinking are toggleable, and
 *  tool results ride the Tool calls checkbox; everything else always shows. */
export function filterLogEntries(
  logs: readonly LogEntry[],
  prefs: ChatFilterPrefs,
): LogEntry[] {
  return logs.filter((e) => {
    if (e.type === 'text') return prefs.showText;
    if (e.type === 'tool_call' || e.type === 'tool_result') return prefs.showToolCalls;
    if (e.type === 'thinking') return prefs.showThinking;
    return true;
  });
}

/**
 * Timestamp-stamp decoration for the visible slice: a text/user entry shows a
 * stamp when its type or HH:MM differs from the previous eligible entry. The
 * dedup chain is positional, so the first eligible entry of any slice always
 * carries its stamp - deliberate, so a revealed window never starts unstamped.
 */
export function decorateLogs(visible: readonly LogEntry[]): Decorated[] {
  const result = new Array<Decorated>(visible.length);
  let lastType: LogEntry['type'] | null = null;
  let lastHHMM: string | null = null;
  for (let i = 0; i < visible.length; i++) {
    const entry = visible[i];
    const eligible = entry.type === 'text' || entry.type === 'user';
    if (!eligible) { result[i] = { entry, showStamp: false }; continue; }
    const hhmm = formatHHMM(entry.ts);
    if (hhmm === null) { result[i] = { entry, showStamp: false }; continue; }
    const showStamp = lastType !== entry.type || lastHHMM !== hhmm;
    lastType = entry.type;
    lastHHMM = hhmm;
    if (!showStamp) { result[i] = { entry, showStamp: false }; continue; }
    const title = formatTitle(entry.ts);
    if (title === null) { result[i] = { entry, showStamp: false }; continue; }
    result[i] = { entry, showStamp: true, hhmm, title };
  }
  return result;
}
