import { useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { Link } from 'react-router';
import type { BoardEvent } from '../../types';
import { useSSEBus } from '../../hooks/useSSEBus';
import { formatRelativeTime } from '../CardPanel/utils';
import { DeckPanel } from './DeckPanel';
import { isHumanAgent, projectForCardId, stateColor } from './utils';

interface ActivityFeedProps {
  prefixMap: Map<string, string>;
}

interface FeedEntry {
  /** Stable React key - never reused, never collides. */
  id: string;
  /** Server-side wall-clock millis for ordering. NaN-tolerant. */
  tsMs: number;
  event: BoardEvent;
}

const MAX_ENTRIES = 20;
/** Maximum number of keys held in the dedupe FIFO - twice the visible window. */
const DEDUPE_CAP = MAX_ENTRIES * 2;

const TRACKED: ReadonlySet<BoardEvent['type']> = new Set<BoardEvent['type']>([
  'card.state_changed',
  'card.claimed',
  'card.released',
  'card.stalled',
  'card.log_added',
]);

interface RenderedBody {
  state?: string;
  prefix: string;
  agent: string;
}

function renderEvent(event: BoardEvent): RenderedBody {
  const agent = event.agent ?? ' - ';
  switch (event.type) {
    case 'card.state_changed': {
      const to = typeof event.data?.to_state === 'string' ? event.data.to_state : '';
      return { state: to, prefix: '→', agent };
    }
    case 'card.claimed':
      return { prefix: 'claimed by', agent };
    case 'card.released':
      return { prefix: 'released by', agent };
    case 'card.stalled':
      return { state: 'stalled', prefix: '→', agent: ' - ' };
    case 'card.log_added':
      return { prefix: 'log entry by', agent };
    default:
      return { prefix: event.type, agent };
  }
}

/** Deterministic dedupe key for an event. Two SSE replays of the same event
 *  produce identical keys; legitimate distinct events do not collide. */
function dedupeKey(event: BoardEvent): string {
  const to =
    event.type === 'card.state_changed' && typeof event.data?.to_state === 'string'
      ? event.data.to_state
      : '';
  return `${event.type}|${event.timestamp}|${event.card_id}|${event.agent ?? ''}|${to}`;
}

function makeId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export function ActivityFeed({ prefixMap }: ActivityFeedProps) {
  const { subscribe, connected } = useSSEBus();
  const [entries, setEntries] = useState<FeedEntry[]>([]);
  // 30s ticker forces relative-timestamp re-render without surfacing
  // an unused state variable.
  const [, tick] = useReducer((n: number) => n + 1, 0);
  // Bounded FIFO dedupe: Map preserves insertion order; we evict the oldest
  // key when the map reaches DEDUPE_CAP, so memory stays O(MAX_ENTRIES).
  const dedupeRef = useRef<Map<string, true>>(new Map());

  useEffect(() => {
    const id = window.setInterval(() => tick(), 30_000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    return subscribe('card.*', (event) => {
      if (!TRACKED.has(event.type)) return;
      const key = dedupeKey(event);
      if (dedupeRef.current.has(key)) return;
      // Bounded FIFO: evict the oldest key before adding so the map stays ≤ DEDUPE_CAP.
      if (dedupeRef.current.size >= DEDUPE_CAP) {
        const oldest = dedupeRef.current.keys().next().value;
        if (oldest !== undefined) dedupeRef.current.delete(oldest);
      }
      dedupeRef.current.set(key, true);
      setEntries((prev) => {
        const tsMs = Date.parse(event.timestamp);
        const next: FeedEntry = {
          id: makeId(),
          tsMs: Number.isNaN(tsMs) ? 0 : tsMs,
          event,
        };
        // SSE delivery is in-order; defensive insertion-sort for clock-skew
        // nudges: find the correct position in O(n) rather than sorting O(n log n).
        let insertAt = 0;
        while (insertAt < prev.length && prev[insertAt].tsMs >= next.tsMs) {
          insertAt++;
        }
        const out = [...prev.slice(0, insertAt), next, ...prev.slice(insertAt)];
        if (out.length > MAX_ENTRIES) {
          out.length = MAX_ENTRIES;
        }
        return out;
      });
    });
  }, [subscribe]);

  const liveLabel = useMemo(
    () => (connected ? 'live · SSE' : 'reconnecting'),
    [connected],
  );

  return (
    <DeckPanel
      area="activity"
      accent="var(--purple)"
      title="Activity"
      meta={
        <span style={{ color: connected ? 'var(--aqua)' : 'var(--grey1)' }}>{liveLabel}</span>
      }
    >
      <div className="apd-panel-body" style={{ padding: '0 0 6px' }}>
        {entries.length === 0 ? (
          <div className="apd-panel-empty">Waiting for activity…</div>
        ) : (
          entries.map((entry) => {
            const body = renderEvent(entry.event);
            const relTs = (() => {
              try {
                return formatRelativeTime(entry.event.timestamp);
              } catch {
                return '';
              }
            })();
            const project = projectForCardId(entry.event.card_id, prefixMap);
            const innerContent = (
              <>
                <span
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 10,
                    color: 'var(--grey0)',
                    paddingTop: 2,
                    fontVariantNumeric: 'tabular-nums',
                  }}
                >
                  {relTs}
                </span>
                <span
                  style={{
                    fontSize: 12.5,
                    color: 'var(--grey2)',
                    lineHeight: 1.5,
                    letterSpacing: '-0.005em',
                  }}
                >
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 10.5,
                      color: 'var(--aqua)',
                      fontWeight: 500,
                      letterSpacing: '0.04em',
                    }}
                  >
                    {entry.event.card_id}
                  </span>{' '}
                  {body.state ? (
                    <>
                      <span style={{ color: 'var(--grey0)', margin: '0 4px' }}>
                        {body.prefix}
                      </span>
                      <span
                        style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: 11,
                          fontWeight: 500,
                          color: stateColor(body.state),
                        }}
                      >
                        {body.state}
                      </span>
                    </>
                  ) : (
                    <span style={{ color: 'var(--grey1)' }}>{body.prefix}</span>
                  )}
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 10.5,
                      marginLeft: 6,
                      color: isHumanAgent(body.agent) ? 'var(--purple)' : 'var(--aqua)',
                    }}
                  >
                    {body.agent}
                  </span>
                </span>
              </>
            );
            const rowStyle = {
              display: 'grid',
              gridTemplateColumns: '56px 1fr',
              gap: 10,
              padding: '8px 14px',
              borderBottom: '1px solid var(--bg1)',
              alignItems: 'start',
              textDecoration: 'none',
            } as const;
            return project ? (
              <Link
                key={entry.id}
                to={`/projects/${project}`}
                className="apd-activity-row"
                style={rowStyle}
                aria-label={`${entry.event.card_id} ${body.prefix} ${body.agent}`}
              >
                {innerContent}
              </Link>
            ) : (
              <div
                key={entry.id}
                className="apd-activity-row apd-activity-row-static"
                style={rowStyle}
                aria-label={`${entry.event.card_id} ${body.prefix} ${body.agent}`}
              >
                {innerContent}
              </div>
            );
          })
        )}
      </div>
    </DeckPanel>
  );
}
