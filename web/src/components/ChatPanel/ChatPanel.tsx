import { useCallback, useLayoutEffect, useMemo, useRef } from 'react';
import type { LogEntry } from '../../types';
import { useChatFilterPrefs } from '../../hooks/useChatFilterPrefs';
import { formatHHMM, formatTitle } from '../../utils/chatTimestamp';
import { logRowKey } from '../../utils/logRowKey';
import { ChatComposer } from './ChatComposer';
import { ChatEntry } from './ChatEntry';
import { WorkingIndicator } from './WorkingIndicator';
import type { WorkingState } from '../../hooks/useWorkingState';

const NEAR_BOTTOM_THRESHOLD = 50;

export interface ChatPanelProps {
  logs: readonly LogEntry[];
  onSend: (content: string) => void | Promise<void>;
  sendDisabled: boolean;
  /**
   * Optional footer rendered below the compose row. Card-bound chat uses
   * it for the "Switch to Autonomous" button + read-only indicator. Global
   * chat passes nothing.
   */
  footer?: React.ReactNode;
  /**
   * When non-empty, replaces the compose row with a read-only footer
   * showing the message. Used when status is cold/promoted.
   */
  readOnlyMessage?: string;
  /**
   * Imperative-style focus trigger: whenever this value changes (and the
   * compose textarea is mounted, i.e. not in read-only/cold state), the
   * textarea grabs focus. Multi-pane chat passes the active pane's
   * sessionID - so opening / focusing a pane puts the cursor in its
   * compose box without an extra click. Leave undefined to opt out.
   */
  focusKey?: string | number;
  /**
   * Working-indicator state for the in-flight turn, or null/undefined when
   * idle. Rendered as the last thread entry, independent of the
   * text/tool-call/thinking filter prefs. Card-bound chat passes nothing.
   */
  working?: WorkingState | null;
}

type Decorated =
  | { entry: LogEntry; showStamp: false }
  | { entry: LogEntry; showStamp: true; hhmm: string; title: string };

export function ChatPanel({ logs, onSend, sendDisabled, footer, readOnlyMessage, focusKey, working }: ChatPanelProps) {
  const { prefs, setPref } = useChatFilterPrefs();
  const { showText, showToolCalls, showThinking } = prefs;
  const logContainerRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
  }, []);

  // useLayoutEffect pins the scroll before paint so new content lands at the
  // bottom on the same frame.
  useLayoutEffect(() => {
    const el = logContainerRef.current;
    if (!el) return;
    if (userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [logs, working]);

  const filteredLogs = useMemo(
    () =>
      logs.filter((e) => {
        if (e.type === 'text') return showText;
        if (e.type === 'tool_call' || e.type === 'tool_result') return showToolCalls;
        if (e.type === 'thinking') return showThinking;
        return true;
      }),
    [logs, showText, showToolCalls, showThinking],
  );

  const decoratedLogs = useMemo<Decorated[]>(() => {
    const result = new Array<Decorated>(filteredLogs.length);
    let lastType: LogEntry['type'] | null = null;
    let lastHHMM: string | null = null;
    for (let i = 0; i < filteredLogs.length; i++) {
      const entry = filteredLogs[i];
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
  }, [filteredLogs]);

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Filter bar */}
      <div className="flex items-center gap-4 px-4 py-2 border-b border-[var(--bg3)] bg-[var(--bg1)] text-xs font-mono shrink-0">
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--fg)' }}>
          <input
            type="checkbox"
            checked={showText}
            onChange={(e) => setPref('showText', e.target.checked)}
          />
          Text
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--aqua)' }}>
          <input
            type="checkbox"
            checked={showToolCalls}
            onChange={(e) => setPref('showToolCalls', e.target.checked)}
          />
          Tool calls
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer" style={{ color: 'var(--grey2)' }}>
          <input
            type="checkbox"
            checked={showThinking}
            onChange={(e) => setPref('showThinking', e.target.checked)}
          />
          Thinking
        </label>
      </div>

      {/* Log column */}
      <div
        ref={logContainerRef}
        onScroll={handleLogScroll}
        className="flex-1 min-h-[60px] overflow-y-auto overflow-x-hidden px-4 py-4 space-y-3 bg-[var(--bg-dim)]"
        role="log"
        aria-live="polite"
        aria-label="Chat log"
      >
        {decoratedLogs.length === 0 && !working ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          <>
            {decoratedLogs.map((d) => {
              return (
                <ChatEntry
                  key={logRowKey(d.entry)}
                  entry={d.entry}
                  stampHHMM={d.showStamp ? d.hhmm : undefined}
                  stampTitle={d.showStamp ? d.title : undefined}
                />
              );
            })}
            {working && <WorkingIndicator verb={working.verb} since={working.since} />}
          </>
        )}
      </div>

      {readOnlyMessage ? (
        <div
          className="px-4 py-2 text-xs font-mono italic text-center border-t border-[var(--bg3)]"
          style={{ backgroundColor: 'var(--bg4)', color: 'var(--grey2)' }}
          role="status"
        >
          {readOnlyMessage}
        </div>
      ) : (
        <ChatComposer
          onSend={onSend}
          sendDisabled={sendDisabled}
          footer={footer}
          focusKey={focusKey}
        />
      )}
    </div>
  );
}
