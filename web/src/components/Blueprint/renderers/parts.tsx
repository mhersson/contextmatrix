import type { ManualCheck } from '../artifactData';
import { RevisionCloud } from '../RevisionCloud';

interface ManualChecksProps {
  title: string;
  items: ManualCheck[];
  /** Ids to mark with a revision cloud of `version`. */
  changed?: Set<string>;
  version?: number;
}

export function ManualChecks({ title, items, changed, version }: ManualChecksProps) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className="bp-criteria">
        {items.map((m) => (
          <li key={m.id}>
            {changed?.has(m.id) && version !== undefined ? <RevisionCloud version={version}>{row(m)}</RevisionCloud> : row(m)}
          </li>
        ))}
      </ul>
    </section>
  );
}

function row(m: ManualCheck) {
  return (
    <>
      <span className="bp-id">{m.id}</span> {m.text}
    </>
  );
}

export function TextList({ title, items, tone }: { title: string; items: string[]; tone?: 'red' | 'orange' }) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className={tone ? `bp-list bp-list--${tone}` : 'bp-list'}>
        {items.map((t, i) => (
          <li key={i}>{t}</li>
        ))}
      </ul>
    </section>
  );
}
