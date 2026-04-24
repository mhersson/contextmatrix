import { useEffect, useState } from 'react';
import type { Card } from '../../../types';
import { api } from '../../../api/client';
import { chipClassForState } from '../utils';

interface RelatedCard {
  state: string;
  title: string;
}

interface MetadataRelatedProps {
  card: Card;
  runnerAttached: boolean;
  onSubtaskClick: (cardId: string) => void;
}

/**
 * Related-card sections of the Info rail tab: Parent (subtask cards
 * only), Subtasks, and Depends on. All three share one hydration effect
 * — the combined set of IDs is fetched via `api.getCard` on mount and
 * whenever the id membership changes. `Promise.allSettled` + a per-id
 * catch fallback means one 404 doesn't wipe the whole related map.
 *
 * Effect deps use joined-string markers so SSE updates that rebuild the
 * card object without changing the id membership don't re-fire the fetch.
 */
export function MetadataRelated({
  card,
  runnerAttached,
  onSubtaskClick,
}: MetadataRelatedProps) {
  const [related, setRelated] = useState<Record<string, RelatedCard>>({});

  const subtaskIds = (card.subtasks ?? []).join(',');
  const dependsIds = (card.depends_on ?? []).join(',');
  useEffect(() => {
    const ids = [...(card.subtasks ?? []), ...(card.depends_on ?? [])];
    if (ids.length === 0) return;
    let cancelled = false;
    const out: Record<string, RelatedCard> = {};
    Promise.allSettled(ids.map(async (id) => {
      try {
        const c = await api.getCard(card.project, id);
        out[id] = { state: c.state, title: c.title };
      } catch {
        out[id] = { state: 'unknown', title: id };
      }
    })).then(() => {
      if (!cancelled) setRelated(out);
    });
    return () => { cancelled = true; };
  // eslint-disable-next-line react-hooks/exhaustive-deps -- see subtaskIds/dependsIds above
  }, [subtaskIds, dependsIds, card.project]);

  const subtasks = card.subtasks ?? [];
  const deps = card.depends_on ?? [];

  return (
    <>
      {/* Parent (subtask cards only) */}
      {card.parent && (
        <section className="bf-aside-section">
          <h4>Parent</h4>
          <button
            type="button"
            className="bf-rel-card"
            onClick={() => onSubtaskClick(card.parent!)}
          >
            <span className="bf-rel-id">{card.parent}</span>
            <span className="bf-rel-title">↑ open parent card</span>
          </button>
        </section>
      )}

      {/* Subtasks */}
      {subtasks.length > 0 && (
        <section className="bf-aside-section">
          <h4>Subtasks <span className="font-mono normal-case" style={{ color: 'var(--grey0)', fontWeight: 400, letterSpacing: '0.02em' }}>· {subtasks.length}</span></h4>
          <div className="flex flex-col">
            {subtasks.map((id) => {
              const r = related[id];
              const stateLabel = r?.state ?? '…';
              return (
                <button
                  key={id}
                  type="button"
                  className="bf-rel-card"
                  onClick={() => onSubtaskClick(id)}
                  title={r?.title ?? id}
                >
                  <span
                    className={`chip-pill ${chipClassForState(r?.state ?? 'todo')}`}
                    style={{ fontSize: '10px', padding: '2px 6px' }}
                  >
                    {stateLabel.replace(/_/g, ' ')}
                  </span>
                  <span className="bf-rel-id">{id}</span>
                  <span className="bf-rel-title">{r?.title ?? '…'}</span>
                </button>
              );
            })}
          </div>
        </section>
      )}

      {/* Depends on */}
      {(deps.length > 0 || !runnerAttached) && (
        <section className="bf-aside-section">
          <h4>Depends on{deps.length > 0 ? <span className="font-mono normal-case" style={{ color: 'var(--grey0)', fontWeight: 400, letterSpacing: '0.02em' }}> · {deps.length}</span> : null}</h4>
          {deps.length > 0 ? (
            <div className="flex flex-col">
              {deps.map((id) => {
                const r = related[id];
                const stateLabel = r?.state ?? '…';
                return (
                  <button
                    key={id}
                    type="button"
                    className="bf-rel-card"
                    onClick={() => onSubtaskClick(id)}
                    title={r?.title ?? id}
                  >
                    <span
                      className={`chip-pill ${chipClassForState(r?.state ?? 'todo')}`}
                      style={{ fontSize: '10px', padding: '2px 6px' }}
                    >
                      {stateLabel.replace(/_/g, ' ')}
                    </span>
                    <span className="bf-rel-id">{id}</span>
                    <span className="bf-rel-title">{r?.title ?? '…'}</span>
                  </button>
                );
              })}
            </div>
          ) : (
            <div className="font-mono" style={{ color: 'var(--grey0)', fontSize: '11px' }}>
              No dependencies.
            </div>
          )}
          {!runnerAttached && (
            <button
              type="button"
              className="bf-btn-ghost bf-btn-sm"
              style={{ width: '100%', justifyContent: 'center', marginTop: deps.length > 0 ? '8px' : '6px' }}
              title="Coming soon: dependency picker"
              disabled
            >
              + add dependency
            </button>
          )}
        </section>
      )}
    </>
  );
}
