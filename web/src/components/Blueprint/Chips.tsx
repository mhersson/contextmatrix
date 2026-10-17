import type { ReactNode } from 'react';
import { assumptionChip, basisRefs, sourceTags, type Assumption, type ChipTone } from './artifactData';

export function Chip({ tone, children }: { tone: ChipTone; children: ReactNode }) {
  return <span className={`bp-chip bp-chip--${tone}`}>{children}</span>;
}

export function SourceChips({ text }: { text: string }) {
  return (
    <>
      {sourceTags(text).map((tag) => (
        <Chip key={tag} tone="green">
          {tag}
        </Chip>
      ))}
    </>
  );
}

export function BasisChips({ basis }: { basis: string }) {
  const { refs, rest } = basisRefs(basis);
  return (
    <>
      {refs.map((r) => (
        <Chip key={r} tone={r.startsWith('Q') ? 'blue' : 'green'}>
          {r}
        </Chip>
      ))}
      {rest && <span className="bp-basis-rest">{rest}</span>}
    </>
  );
}

export function AssumptionList({ items }: { items: Assumption[] }) {
  return (
    <ul className="bp-assumptions">
      {items.map((a, i) => {
        const chip = assumptionChip(a);
        return (
          <li key={a.id || i}>
            <span className="bp-assumption-chip">{chip && <Chip tone={chip.tone}>{chip.label}</Chip>}</span>
            <span>{a.text}</span>
          </li>
        );
      })}
    </ul>
  );
}
