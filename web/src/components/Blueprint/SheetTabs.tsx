import { Fragment } from 'react';
import type { SheetTab, SliceTab } from './sheetModel';

interface SheetTabsProps {
  tabs: SheetTab[];
  /** Empty unless a per-slice sheet of a blueprint with slices is open. */
  slices: SliceTab[];
  /** The shown stage; null before anything is on the sheet. */
  stage: string | null;
  /** Separators set the per-slice tabs apart while the blueprint shows slices. */
  grouped: boolean;
  onStage: (stage: string) => void;
  onSlice: (slice: string) => void;
  onClose?: () => void;
}

export function SheetTabs({ tabs, slices, stage, grouped, onStage, onSlice, onClose }: SheetTabsProps) {
  const sub = slices.length > 0;
  const first = tabs.findIndex((t) => t.perSlice);
  const last = tabs.findLastIndex((t) => t.perSlice);
  return (
    <>
      <div className={`bp-tabs${sub ? ' bp-tabs--sub' : ''}`} role="tablist" aria-label="Artifacts">
        {tabs.map((t, i) => {
          const on = stage === t.stage;
          return (
            <Fragment key={t.stage}>
              {grouped && i === first && <span className="bp-tabs-sep" aria-hidden="true" />}
              <button
                type="button"
                role="tab"
                aria-selected={on}
                disabled={!t.enabled}
                className={`bp-tab${on ? ' bp-tab--on' : ''}${on && sub ? ' bp-tab--joined' : ''}`}
                onClick={() => onStage(t.stage)}
              >
                {t.label}
                {t.current && (
                  <span className="bp-tab-dot">
                    <span className="sr-only">, current stage</span>
                  </span>
                )}
              </button>
              {grouped && i === last && <span className="bp-tabs-sep" aria-hidden="true" />}
            </Fragment>
          );
        })}
        <span className="bp-tabs-fill" />
        {onClose && (
          <button type="button" className="bp-tabs-close" onClick={onClose}>
            Close
          </button>
        )}
      </div>
      {sub && (
        <div className="bp-slices" role="tablist" aria-label="Slices">
          {slices.map((s) => (
            <button
              key={s.id}
              type="button"
              role="tab"
              aria-selected={s.selected}
              disabled={!s.enabled}
              className={`bp-slice${s.selected ? ' bp-slice--on' : ''}${s.waiting ? ' bp-slice--wait' : ''}`}
              onClick={() => onSlice(s.id)}
            >
              {s.id} <small>{s.title}</small>
              {s.waiting && <span className="sr-only">, waiting for you</span>}
            </button>
          ))}
        </div>
      )}
    </>
  );
}
