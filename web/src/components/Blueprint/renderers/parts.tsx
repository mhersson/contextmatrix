import type { ManualCheck } from '../artifactData';

export function ManualChecks({ title, items }: { title: string; items: ManualCheck[] }) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className="bp-criteria">
        {items.map((m) => (
          <li key={m.id}>
            <span className="bp-id">{m.id}</span> {m.text}
          </li>
        ))}
      </ul>
    </section>
  );
}
