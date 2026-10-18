import type { RevisionMode } from '../BodyMarkdown';
import { BasisChips } from '../Chips';
import { DiffText } from '../DiffText';
import { RevisionCloud } from '../RevisionCloud';
import type { Decision } from '../artifactData';

function DecisionBlock({ decision: d, before, compare }: { decision: Decision; before: Decision | null; compare: boolean }) {
  return (
    <div className={`bp-dec${compare && before === null ? ' bp-dec--added' : ''}`}>
      <div className="bp-dec-head">
        <span className="bp-id">{d.id}</span> {before ? <DiffText before={before.text} after={d.text} /> : d.text}
      </div>
      <p>
        <span className="bp-dec-label">Chosen:</span> {before ? <DiffText before={before.chosen} after={d.chosen} /> : d.chosen}
      </p>
      {d.rejected.length > 0 && (
        <ul className="bp-rejected">
          {d.rejected.map((r, i) => (
            <li key={i}>
              Rejected: {r.option}
              {r.why && ` (${r.why})`}
            </li>
          ))}
        </ul>
      )}
      {d.basis && (
        <div className="bp-meta">
          <span className="bp-dec-label">Basis</span> <BasisChips basis={d.basis} />
        </div>
      )}
    </div>
  );
}

interface DecisionListProps {
  decisions: Decision[];
  /** The previous version's decisions by id, for word diffs in compare. */
  beforeById: Map<string, Decision>;
  /** Decisions changed or new since the previous version; clouded in clouds mode. */
  changed: Set<string>;
  /** The previous version's decisions gone from this list; struck in compare. */
  removed: Decision[];
  mode: RevisionMode;
  version: number;
}

export function DecisionList({ decisions, beforeById, changed, removed, mode, version }: DecisionListProps) {
  const compare = mode === 'compare';
  return (
    <>
      {decisions.map((d) => {
        const block = <DecisionBlock decision={d} before={compare ? (beforeById.get(d.id) ?? null) : null} compare={compare} />;
        return changed.has(d.id) ? (
          <RevisionCloud key={d.id} version={version}>
            {block}
          </RevisionCloud>
        ) : (
          <div key={d.id}>{block}</div>
        );
      })}
      {compare &&
        removed.map((d) => (
          <div key={d.id} className="bp-dec bp-dec--removed">
            <del>
              {d.id} {d.text}
            </del>
          </div>
        ))}
    </>
  );
}

export function ContractList({ contracts }: { contracts: string[] }) {
  if (contracts.length === 0) return null;
  return (
    <>
      <h4>Contracts</h4>
      <ul className="bp-list">
        {contracts.map((c, i) => (
          <li key={i}>{c}</li>
        ))}
      </ul>
    </>
  );
}
