import { useMemo } from 'react';
import type { LogEntry } from '../../types';
import { useChatFilterPrefs } from '../../hooks/useChatFilterPrefs';
import { ChatComposer } from './ChatComposer';
import { ChatTranscript } from './ChatTranscript';
import { filterLogEntries } from './decorateLogs';
import type { WorkingState } from '../../hooks/useWorkingState';

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

export function ChatPanel({ logs, onSend, sendDisabled, footer, readOnlyMessage, focusKey, working }: ChatPanelProps) {
  const { prefs, setPref } = useChatFilterPrefs();
  const { showText, showToolCalls, showThinking } = prefs;

  const filteredLogs = useMemo(
    () => filterLogEntries(logs, prefs),
    // Depend on the individual booleans - prefs is a fresh object per render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [logs, showText, showToolCalls, showThinking],
  );

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

      <ChatTranscript filteredLogs={filteredLogs} working={working} />

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
