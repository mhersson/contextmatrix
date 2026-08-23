import { useEffect, useMemo, useRef, useState } from 'react';
import type { Card, ProjectConfig } from '../../../types';
import { api } from '../../../api/client';
import { chipClassForState } from '../utils';
import { CardPicker } from '../../Playbooks/CardPicker';
import { useMenuDismiss } from '../../../hooks/useMenuDismiss';

interface RelatedCard {
  state: string;
  title: string;
}

interface MetadataRelatedProps {
  card: Card;
  config: ProjectConfig;
  workerAttached: boolean;
  cards: Card[];
  onSubtaskClick: (cardId: string) => void;
  onDependsOnChange: (ids: string[]) => Promise<void>;
}

/**
 * Related-card sections of the Info rail tab: Parent (subtask cards only,
 * rendered id-only - it never reads `related`), Subtasks, and Depends on.
 * Subtasks and Depends-on chip labels share one hydration effect - the
 * combined set of `card.subtasks` + `card.depends_on` IDs is fetched via
 * `api.getCard` on mount and whenever the id membership changes.
 * `Promise.allSettled` + a per-id catch fallback means one 404 doesn't wipe
 * the whole related map.
 *
 * The "+ add dependency" trigger opens a popover (`useMenuDismiss` closes it
 * on outside click / Escape) hosting `CardPicker`, filtered client-side
 * against the `cards` prop threaded down from `ProjectShell`'s SSE-fed board
 * list - no separate fetch, no staleness. Add/remove disable the trigger and
 * every remove button for the duration of the in-flight `onDependsOnChange`
 * call and compute the next list from the `card.depends_on` prop, never a
 * local copy: the controls stay disabled until the promise settles, so by
 * the time they re-enable the parent's `updateCardLocally` has already
 * applied the response (or, on rejection, `card.depends_on` was never
 * touched) - either way the next click reads the current source of truth
 * instead of a second copy that could diverge from it.
 *
 * Effect deps use joined-string markers so SSE updates that rebuild the
 * card object without changing the id membership don't re-fire the fetch.
 */
export function MetadataRelated({
  card,
  config,
  workerAttached,
  cards,
  onSubtaskClick,
  onDependsOnChange,
}: MetadataRelatedProps) {
  const [related, setRelated] = useState<Record<string, RelatedCard>>({});
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerFilter, setPickerFilter] = useState('');
  const [saving, setSaving] = useState(false);
  const pickerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  // Guard against setState after unmount: the panel can close while a save
  // is still in flight (same pattern as CardChat.tsx's promotion guard).
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => { aliveRef.current = false; };
  }, []);

  const subtaskIds = (card.subtasks ?? []).join(',');
  const dependsIds = (card.depends_on ?? []).join(',');
  useEffect(() => {
    const ids = [...(card.subtasks ?? []), ...(card.depends_on ?? [])];
    if (ids.length === 0) return;
    const controller = new AbortController();
    const { signal } = controller;
    const out: Record<string, RelatedCard> = {};
    Promise.allSettled(ids.map(async (id) => {
      try {
        const c = await api.getCard(card.project, id, signal);
        out[id] = { state: c.state, title: c.title };
      } catch (err) {
        // Ignore abort errors - cleanup fired before the request completed.
        if (err instanceof DOMException && err.name === 'AbortError') return;
        out[id] = { state: 'unknown', title: id };
      }
    })).then(() => {
      if (!signal.aborted) setRelated(out);
    });
    return () => { controller.abort(); };
    // subtaskIds / dependsIds are value-stable joined strings derived from
    // card.subtasks / card.depends_on; listing the arrays directly would
    // re-fire the effect on every SSE-driven card identity change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subtaskIds, dependsIds, card.project]);

  const subtasks = card.subtasks ?? [];
  const deps = card.depends_on ?? [];

  const excludeIds = useMemo(
    () => new Set([card.id, ...deps, ...subtasks]),
    // deps / subtasks are new arrays every render; dependsIds / subtaskIds
    // are the value-stable joined markers already used by the hydration
    // effect above, so this only recomputes when membership actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [card.id, dependsIds, subtaskIds],
  );

  const closePicker = () => setPickerOpen(false);
  useMenuDismiss(pickerRef, pickerOpen, closePicker, triggerRef);

  const submit = (next: string[]) => {
    // Re-entrancy guard: the trigger and remove buttons are disabled while
    // saving, but the picker's result buttons are not, and a keyboard user can
    // reach them with the popover still open - close it so no second save can
    // start from a stale depends_on.
    if (saving) return;
    setPickerOpen(false);
    setSaving(true);
    // onDependsOnChange (CardPanel.handleDependsOnChange) already swallows
    // its own rejection so the save-error toast stays handleCardSave's job;
    // catch defensively here too so a differently-behaved caller can't leave
    // this in flight forever or surface an unhandled rejection.
    onDependsOnChange(next)
      .catch(() => {})
      .finally(() => {
        if (!aliveRef.current) return;
        setSaving(false);
        // Disabling the focused control (or unmounting the popover) drops
        // focus to <body>, which the panel's focus trap does not wrap from.
        if (document.activeElement === document.body) triggerRef.current?.focus();
      });
  };

  const removeDep = (id: string) => {
    submit((card.depends_on ?? []).filter((d) => d !== id));
  };

  const addDep = (id: string) => {
    submit([...(card.depends_on ?? []), id]);
  };

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
      {(deps.length > 0 || !workerAttached) && (
        <section className="bf-aside-section">
          <h4>Depends on{deps.length > 0 ? <span className="font-mono normal-case" style={{ color: 'var(--grey0)', fontWeight: 400, letterSpacing: '0.02em' }}> · {deps.length}</span> : null}</h4>
          {deps.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              {deps.map((id) => {
                const r = related[id];
                const stateLabel = r?.state ?? '…';
                return (
                  <div key={id} className="flex items-center gap-1">
                    <button
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
                    {!workerAttached && (
                      <button
                        type="button"
                        aria-label={`Remove dependency ${id}`}
                        onClick={() => removeDep(id)}
                        disabled={saving}
                        className="hover:text-[var(--red)] transition-colors leading-none disabled:opacity-50"
                        style={{ color: 'var(--grey0)', fontSize: '14px', padding: '2px 4px', flexShrink: 0 }}
                      >
                        ×
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="font-mono" style={{ color: 'var(--grey0)', fontSize: '11px' }}>
              No dependencies.
            </div>
          )}
          {!workerAttached && (
            <div className="relative" ref={pickerRef}>
              <button
                ref={triggerRef}
                type="button"
                className="bf-btn-ghost bf-btn-sm"
                style={{ width: '100%', justifyContent: 'center', marginTop: deps.length > 0 ? '8px' : '6px' }}
                onClick={() => setPickerOpen((open) => !open)}
                disabled={saving}
                aria-expanded={pickerOpen}
                aria-haspopup="dialog"
              >
                + add dependency
              </button>
              {pickerOpen && (
                <div
                  role="dialog"
                  aria-label="Add dependency"
                  className="absolute z-10 w-full mt-1 rounded p-2"
                  style={{ backgroundColor: 'var(--bg1)', border: '1px solid var(--bg3)' }}
                >
                  <CardPicker
                    projects={[config]}
                    project={card.project}
                    onProjectChange={() => {}}
                    cards={cards}
                    filter={pickerFilter}
                    onFilterChange={setPickerFilter}
                    selectedCard={null}
                    onSelectCard={(c) => {
                      setPickerOpen(false);
                      setPickerFilter('');
                      addDep(c.id);
                    }}
                    excludeIds={excludeIds}
                    autoFocus
                  />
                </div>
              )}
            </div>
          )}
        </section>
      )}
    </>
  );
}
