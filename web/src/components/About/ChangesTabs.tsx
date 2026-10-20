import { useState } from 'react';
import { changeKind, changesOf, componentLabel, formatMergedDate, newCount, type SeenMap } from '../../lib/about';
import type { AboutComponent } from '../../types';
import { NewDot } from './NewDot';

const muted = { color: 'var(--blueprint-quiet-strong)' };
const KIND_STYLE = {
  feat: { color: 'var(--blueprint-ink-blue)', ring: 'ring-[var(--blue)]' },
  fix: { color: 'var(--blueprint-ink-orange)', ring: 'ring-[var(--orange)]' },
  change: { color: 'var(--fg)', ring: 'ring-[var(--bg3)]' },
} as const;

function Empty({ c }: { c: AboutComponent }) {
  let text: string | null = null;
  if (c.status === 'unreachable') text = `${componentLabel(c.name)} is not reachable right now.`;
  else if (!c.build) text = 'Version unknown: this build does not report its changes.';
  else if (changesOf(c).length === 0) text = 'No change history was recorded for this build.';
  return text ? (
    <p className="py-3 text-sm" style={muted}>
      {text}
    </p>
  ) : null;
}

export function ChangesTabs({ components, snapshot }: { components: AboutComponent[]; snapshot: SeenMap }) {
  const [active, setActive] = useState(0);
  const current = components[active] ?? components[0];
  if (!current) return null;
  const changes = current.status === 'unreachable' ? [] : changesOf(current);
  const fresh = newCount(changes, snapshot[current.name]);

  return (
    <div>
      <div role="tablist" aria-label="Changes by component" className="grid grid-cols-4">
        {components.map((c, i) => {
          const on = i === active;
          const dot = c.status !== 'unreachable' && newCount(changesOf(c), snapshot[c.name]) > 0;
          return (
            <button
              key={`${c.name}-${i}`}
              type="button"
              role="tab"
              id={`about-tab-${i}`}
              aria-selected={on}
              aria-controls="about-panel"
              onClick={() => setActive(i)}
              className="flex items-center gap-1.5 border-b-2 px-1 py-1.5 text-left text-sm"
              style={{ color: on ? 'var(--fg)' : 'var(--blueprint-quiet-strong)', borderColor: on ? 'var(--green)' : 'var(--bg3)' }}
            >
              {componentLabel(c.name)}
              {dot && <NewDot label="(new changes)" />}
            </button>
          );
        })}
      </div>
      <div role="tabpanel" id="about-panel" aria-labelledby={`about-tab-${active}`} className="max-h-[50vh] overflow-y-auto">
        <Empty c={current} />
        <ul>
          {changes.map((ch, i) => {
            const k = KIND_STYLE[changeKind(ch.kind)];
            return (
              <li key={ch.number} className="grid grid-cols-[3rem_1fr_auto] items-center gap-2 py-1.5 text-sm">
                <span>
                  {i < fresh && (
                    <span
                      className="rounded px-1 text-[0.65rem] ring-1 ring-[var(--green)]"
                      style={{ color: 'var(--fg)', backgroundColor: 'var(--bg-green)' }}
                    >
                      NEW
                    </span>
                  )}
                </span>
                <span className="flex min-w-0 items-baseline gap-2" style={{ color: 'var(--fg)' }}>
                  <span className={`shrink-0 rounded px-1 text-[0.65rem] ring-1 ${k.ring}`} style={{ color: k.color }}>
                    {changeKind(ch.kind)}
                  </span>
                  <span className="truncate">{ch.title}</span>
                  <span className="font-mono text-xs" style={muted}>
                    #{ch.number}
                  </span>
                </span>
                <span className="text-xs" style={muted}>
                  {formatMergedDate(ch.merged_at)}
                </span>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
