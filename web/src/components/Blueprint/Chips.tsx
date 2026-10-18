import type { ReactNode } from 'react';
import { assumptionChip, basisRefs, refTone, sourceTags, type Assumption, type ChipTone } from './artifactData';

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
        <Chip key={r} tone={refTone(r)}>
          {r}
        </Chip>
      ))}
      {rest && <span className="bp-basis-rest">{rest}</span>}
    </>
  );
}

export function StatusChip({ assumption }: { assumption: Assumption }) {
  const chip = assumptionChip(assumption);
  return chip && <Chip tone={chip.tone}>{chip.label}</Chip>;
}
