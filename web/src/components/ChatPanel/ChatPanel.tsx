import { Suspense, lazy, useCallback, useLayoutEffect, useMemo, useRef } from 'react';
import type { LogEntry } from '../../types';
import { useChatFilterPrefs } from '../../hooks/useChatFilterPrefs';
import { safeUrlTransform } from '../../utils/safeUrlTransform';
import { formatHHMM, formatTitle, TimestampLabel } from '../../utils/chatTimestamp';
import { idColor } from '../../utils/colorHash';
import { ChatComposer } from './ChatComposer';

// Lazy-load the markdown previewer so the chat panel doesn't pay the
// bundle cost until first use. The chat markdown styling is fully driven by
// CSS custom properties, so dark/light switches automatically without
// data-color-mode.
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

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
}

type Decorated =
  | { entry: LogEntry; showStamp: false }
  | { entry: LogEntry; showStamp: true; hhmm: string; title: string };

export function ChatPanel({ logs, onSend, sendDisabled, footer, readOnlyMessage, focusKey }: ChatPanelProps) {
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
  }, [logs]);

  const filteredLogs = useMemo(
    () =>
      logs.filter((e) => {
        if (e.type === 'text') return showText;
        if (e.type === 'tool_call') return showToolCalls;
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
        {decoratedLogs.length === 0 ? (
          <div className="text-xs text-[var(--grey1)] italic font-mono">No messages yet.</div>
        ) : (
          decoratedLogs.map((d) => {
            return (
              <ChatEntry
                key={d.entry.seq ?? d.entry.ts}
                entry={d.entry}
                stamp={d.showStamp ? { hhmm: d.hhmm, title: d.title } : null}
              />
            );
          })
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

function ChatEntry({ entry, stamp }: { entry: LogEntry; stamp: { hhmm: string; title: string } | null }) {
  // Structural divider sentinel (kind="divider") rendered as a horizontal
  // rule with a small inline label rather than the normal system message
  // style. The match is on kind (not content) so the rendering survives
  // localised label changes and is unambiguous on REST-bootstrap reload.
  if (entry.kind === 'divider') {
    return (
      <div
        className="flex items-center gap-3 py-2"
        data-testid="chat-divider"
        role="separator"
        aria-label={entry.content || 'divider'}
      >
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
        <span
          className="text-[10px] uppercase tracking-wider font-mono"
          style={{ color: 'var(--grey1)' }}
        >
          {entry.content || 'divider'}
        </span>
        <hr className="flex-1 border-t" style={{ borderColor: 'var(--bg3)' }} />
      </div>
    );
  }

  if (entry.type === 'user') {
    return (
      <div className="flex justify-end">
        <div className="flex flex-col items-end max-w-[85%]">
          {stamp && <TimestampLabel hhmm={stamp.hhmm} title={stamp.title} dateTime={entry.ts} />}
          <div
            className="rounded-lg px-3 py-2 text-sm whitespace-pre-wrap break-words"
            style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--fg)' }}
          >
            {entry.content}
          </div>
        </div>
      </div>
    );
  }

  if (entry.type === 'text') {
    return (
      <div className="flex justify-start">
        <div className="flex flex-col items-start max-w-[85%]">
          {stamp && <TimestampLabel hhmm={stamp.hhmm} title={stamp.title} dateTime={entry.ts} />}
          {entry.agent && <SpeakerChip author={entry.agent} model={entry.model} />}
          <div
            className="rounded-lg px-3 py-2 text-sm break-words"
            style={{ backgroundColor: 'var(--bg2)', color: 'var(--fg)' }}
          >
            <ChatMarkdown source={entry.content} />
          </div>
        </div>
      </div>
    );
  }

  if (entry.type === 'system') {
    return (
      <div
        className="pl-3 border-l-2 text-sm leading-relaxed break-words"
        style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
      >
        <ChatMarkdown source={entry.content} />
      </div>
    );
  }

  return (
    <div
      className="pl-3 border-l-2 text-sm text-[var(--fg)] font-mono leading-relaxed whitespace-pre-wrap break-words"
      style={{ borderLeftColor: accentFor(entry.type), color: textFor(entry.type) }}
    >
      {entry.content}
    </div>
  );
}

/**
 * Speaker attribution for mob session discussion messages. The speaker hue is a
 * deterministic bucket over the shared 5-accent palette (idColor hashes the
 * author name onto CSS custom properties), so the same author always gets
 * the same color and no hex ever appears here.
 *
 * When `model` is present and non-empty, a second pill is rendered on the
 * same line (flex row) beside the speaker pill. The model pill uses the
 * `--purple` accent - the same semantic token the Automation tab uses for
 * mob-phase chips (background `--bg-purple`, text `--purple`) - so it reads
 * as a consistent, mob-related accent regardless of the author's own color.
 * Long model slugs (e.g. `z-ai/glm-5.2`) wrap naturally; truncation, if
 * needed on narrow panes, is a follow-up.
 */
function SpeakerChip({ author, model }: { author: string; model?: string }) {
  const accent = idColor(author);
  const showModel = typeof model === 'string' && model.length > 0;
  return (
    <div className="flex flex-row items-center gap-1.5" style={{ marginBottom: '2px' }}>
      <span
        className="chip-pill font-mono"
        data-testid="speaker-chip"
        style={{
          backgroundColor: `color-mix(in srgb, ${accent} 16%, transparent)`,
          color: accent,
          fontSize: '10px',
        }}
        title={`Speaker: ${author}`}
      >
        {author}
      </span>
      {showModel && (
        <span
          className="chip-pill font-mono"
          data-testid="model-chip"
          style={{
            backgroundColor: 'var(--bg-purple)',
            color: 'var(--purple)',
            fontSize: '10px',
          }}
          title={`Model: ${model}`}
        >
          {model}
        </span>
      )}
    </div>
  );
}

function ChatMarkdown({ source }: { source: string }) {
  return (
    <div className="bf-chat-markdown">
      <Suspense fallback={<div className="whitespace-pre-wrap break-words text-sm">{source}</div>}>
        <MarkdownPreview source={source} skipHtml urlTransform={safeUrlTransform} />
      </Suspense>
    </div>
  );
}

function accentFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--bg3)';
  }
}

function textFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--fg)';
  }
}
