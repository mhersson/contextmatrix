import { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { LogEntry, RoundReply } from '../../types';
import { useChatFilterPrefs } from '../../hooks/useChatFilterPrefs';
import { ChatComposer } from './ChatComposer';
import { CHAT_MESSAGE_MAX_BYTES } from './chatEntryUtils';
import { ChatTranscript } from './ChatTranscript';
import { filterLogEntries } from './decorateLogs';
import { derivePromptStates } from './prompts';
import { deriveHandoffStates, parseHandoffEntry } from './handoff';
import { HandoffContext } from './handoffContext';
import type { WorkingState } from '../../hooks/useWorkingState';
import { RoundBlock } from '../QuestionRound/RoundBlock';
import { pruneSession } from '../QuestionRound/roundDraftStore';
import { COMMAND_KIND, deriveRounds, ROUND_CHAT_KIND, type RoundStatus } from '../QuestionRound/rounds';

// Older frames of a round never show; the waiting round docks below the transcript instead of in it.
const OUT_OF_TRANSCRIPT: ReadonlySet<RoundStatus | undefined> = new Set<RoundStatus | undefined>(['history', 'open', 'paused']);

export interface ChatPanelProps {
  logs: readonly LogEntry[];
  onSend: (content: string) => void | Promise<void>;
  sendDisabled: boolean;
  /** Longest composer message in UTF-8 bytes. Defaults to the chat limit. */
  maxMessageBytes?: number;
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
  /**
   * Lazy-history seam (global chat only; card chat passes nothing): whether
   * older persisted messages exist beyond the in-memory window, whether a
   * page fetch is in flight, and the fetch trigger.
   */
  hasMoreHistory?: boolean;
  loadingOlder?: boolean;
  onLoadOlder?: () => Promise<void> | void;
  /**
   * Whether an open architect prompt takes a click: the session accepts
   * sends and its stream is connected. Card-bound chat passes nothing.
   */
  promptsLive?: boolean;
  /** Keeps a waiting round's unsent picks in this browser under this session. Global chat passes it. */
  sessionID?: string;
  /**
   * Sends a question round's answers as the readable summary plus the structured reply. Set on architect sessions;
   * without it a waiting round still docks, read-only.
   */
  onSendRound?: (content: string, reply: RoundReply) => Promise<void>;
  /** Sends a message typed in the waiting round's chat box, with that round's prompt id. Set on architect sessions. */
  onSendRoundChat?: (content: string, promptId: string) => Promise<void>;
}

export function ChatPanel({
  logs,
  onSend,
  sendDisabled,
  maxMessageBytes = CHAT_MESSAGE_MAX_BYTES,
  footer,
  readOnlyMessage,
  focusKey,
  working,
  hasMoreHistory,
  loadingOlder,
  onLoadOlder,
  promptsLive = false,
  sessionID,
  onSendRound,
  onSendRoundChat,
}: ChatPanelProps) {
  const { prefs, setPref } = useChatFilterPrefs();
  const { showText, showToolCalls, showThinking } = prefs;

  const filteredLogs = useMemo(
    () => filterLogEntries(logs, prefs),
    // Depend on the individual booleans - prefs is a fresh object per render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [logs, showText, showToolCalls, showThinking],
  );

  // Over the whole ring, not the filtered tail: a reply further down closes
  // a prompt even when the reply's row is filtered or not yet revealed.
  const promptStates = useMemo(() => derivePromptStates(logs, promptsLive), [logs, promptsLive]);

  const handoffCtx = useContext(HandoffContext);
  const handoffSessions = handoffCtx?.sessions;
  const handoffStates = useMemo(
    () => deriveHandoffStates(logs, sessionID, handoffSessions ?? []),
    [logs, sessionID, handoffSessions],
  );

  const rounds = useMemo(() => deriveRounds(logs, promptsLive), [logs, promptsLive]);
  const transcriptLogs = useMemo(
    () =>
      filteredLogs.filter(
        (e) =>
          e.kind !== COMMAND_KIND &&
          (e.kind !== ROUND_CHAT_KIND || rounds.stray.has(e)) &&
          !rounds.answered.has(e) &&
          !OUT_OF_TRANSCRIPT.has(rounds.byEntry.get(e)),
      ),
    [filteredLogs, rounds],
  );
  const docked = rounds.pending;
  const dockedId = docked?.id ?? null;
  const hasRounds = rounds.byEntry.size > 0;
  // A closed or superseded round keeps no draft; with no round frame loaded yet nothing is known, so nothing is dropped.
  useEffect(() => {
    if (sessionID !== undefined && hasRounds) pruneSession(sessionID, dockedId);
  }, [sessionID, dockedId, hasRounds]);
  const sendRoundChat = docked && onSendRoundChat ? (text: string) => onSendRoundChat(text, docked.id) : undefined;

  // Ref-stable, so memoized rows never re-render on the parent's per-render
  // onSend. A sent reply returns focus to the composer, as a send does.
  const onSendRef = useRef(onSend);
  useEffect(() => {
    onSendRef.current = onSend;
  });
  const [refocus, setRefocus] = useState(0);
  const sendReply = useCallback(async (text: string) => {
    await onSendRef.current(text);
    setRefocus((n) => n + 1);
  }, []);

  // Ref-stable like sendReply, so memoized rows keep their props across renders.
  const handoffRef = useRef({ ctx: handoffCtx, sessionID });
  useEffect(() => {
    handoffRef.current = { ctx: handoffCtx, sessionID };
  });
  const reviewHandoff = useCallback((entry: LogEntry) => {
    const { ctx, sessionID: chatId } = handoffRef.current;
    const handoff = parseHandoffEntry(entry);
    if (!ctx || !chatId || !handoff || entry.seq === undefined) return;
    const chatTitle = ctx.sessions.find((s) => s.id === chatId)?.title.trim() || '(untitled)';
    ctx.openHandoff({ chatId, chatTitle, seq: entry.seq, ...handoff });
  }, []);

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

      <ChatTranscript
        filteredLogs={transcriptLogs}
        roundStates={rounds.byEntry}
        working={working}
        hasMoreHistory={hasMoreHistory}
        loadingOlder={loadingOlder}
        onLoadOlder={onLoadOlder}
        promptStates={promptStates}
        onReply={sendReply}
        handoffStates={handoffStates}
        onReviewHandoff={handoffCtx ? reviewHandoff : undefined}
      />

      {docked && (
        <RoundBlock
          key={`${sessionID ?? ''}/${docked.id}`}
          draftScope={sessionID}
          promptId={docked.id}
          round={docked.round}
          mode={onSendRound && docked.status === 'open' ? 'open' : 'paused'}
          onSendRound={onSendRound}
          onSendChat={sendRoundChat}
          sentChat={docked.sent.map((e) => e.content)}
          chatDisabled={sendDisabled}
          focusKey={focusKey}
        />
      )}
      {readOnlyMessage ? (
        <div
          className="px-4 py-2 text-xs font-mono italic text-center border-t border-[var(--bg3)]"
          style={{ backgroundColor: 'var(--bg4)', color: 'var(--grey2)' }}
          role="status"
        >
          {readOnlyMessage}
        </div>
      ) : docked ? null : (
        <ChatComposer
          onSend={onSend}
          sendDisabled={sendDisabled}
          maxBytes={maxMessageBytes}
          footer={footer}
          focusKey={focusKey}
          refocus={refocus}
        />
      )}
    </div>
  );
}
