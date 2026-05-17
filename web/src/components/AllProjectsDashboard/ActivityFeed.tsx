import { useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { BoardEvent } from '../../types';
import { useSSEBus } from '../../hooks/useSSEBus';
import { formatRelativeTime } from '../CardPanel/utils';
import { projectForCardId, stateColor } from './utils';

interface ActivityFeedProps {
  prefixMap: Map<string, string>;
}

interface FeedEntry {
  /** Stable React key — never reused, never collides. */
  id: string;
  /** Server-side wall-clock millis for ordering. NaN-tolerant. */
  tsMs: number;
  event: BoardEvent;
}

const MAX_ENTRIES = 20;

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
  const agent = event.agent ?? '—';
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
      return { state: 'stalled', prefix: '→', agent: '—' };
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
  // Survives across effect re-subscribes; can't collide with itself.
  const dedupeRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    const id = window.setInterval(() => tick(), 30_000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    return subscribe('card.*', (event) => {
      if (!TRACKED.has(event.type)) return;
      const key = dedupeKey(event);
      if (dedupeRef.current.has(key)) return;
      dedupeRef.current.add(key);
      setEntries((prev) => {
        const tsMs = Date.parse(event.timestamp);
        const next: FeedEntry = {
          id: makeId(),
          tsMs: Number.isNaN(tsMs) ? 0 : tsMs,
          event,
        };
        const out = [next, ...prev];
        // Defensive sort: SSE delivery is in-order but a clock-skew nudge or
        // server-side reorder should not put older events at the top.
        out.sort((a, b) => b.tsMs - a.tsMs);
        if (out.length > MAX_ENTRIES) {
          // Evict the dedupe entries for dropped events so the set can't grow
          // unbounded over a long session.
          for (let i = MAX_ENTRIES; i < out.length; i++) {
            dedupeRef.current.delete(dedupeKey(out[i].event));
          }
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
    <section
      className="apd-card"
      style={{
        borderColor: 'var(--bg3)',
        backgroundColor: 'var(--bg1)',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        className="flex items-center justify-between"
        style={{
          padding: '16px 20px 14px',
          borderBottom: '1px solid var(--bg2)',
        }}
      >
        <div className="flex items-baseline gap-2.5">
          <h2 className="apd-section-title">Recent activity</h2>
          <span
            className="apd-count"
            style={{
              color: connected ? 'var(--aqua)' : 'var(--grey1)',
              backgroundColor: connected ? 'var(--bg-aqua)' : 'var(--bg2)',
              border: connected ? '1px solid transparent' : '1px solid var(--bg3)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            {liveLabel}
          </span>
        </div>
      </div>
      <div className="flex flex-col" style={{ flex: 1, minHeight: 0, padding: '6px 0' }}>
        {entries.length === 0 ? (
          <div
            style={{
              padding: '32px 20px',
              textAlign: 'center',
              fontSize: 12.5,
              color: 'var(--grey0)',
              fontStyle: 'italic',
            }}
          >
            Waiting for activity…
          </div>
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
                    fontSize: 11,
                    color: 'var(--grey1)',
                    paddingTop: 2,
                  }}
                >
                  {relTs}
                </span>
                <span
                  style={{
                    fontSize: 13,
                    color: 'var(--grey2)',
                    lineHeight: 1.5,
                    letterSpacing: '-0.005em',
                  }}
                >
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 11.5,
                      color: 'var(--aqua)',
                      fontWeight: 500,
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
                          fontSize: 11.5,
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
                      fontSize: 11.5,
                      color: 'var(--grey1)',
                      marginLeft: 6,
                    }}
                  >
                    {body.agent}
                  </span>
                </span>
              </>
            );
            const rowStyle = {
              display: 'grid',
              gridTemplateColumns: '64px 1fr',
              gap: 12,
              padding: '11px 20px',
              borderBottom: '1px solid var(--bg2)',
              alignItems: 'start',
              textDecoration: 'none',
            } as const;
            return project ? (
              <Link
                key={entry.id}
                to={`/projects/${project}/dashboard`}
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
    </section>
  );
}
