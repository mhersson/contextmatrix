import { BodyMarkdown } from '../BodyMarkdown';
import { SourceChips } from '../Chips';
import { RevisionCloud } from '../RevisionCloud';
import { parseGrounding, withoutTags, type GroundingProject, type Verdict } from '../artifactData';
import { changedById } from '../revisions';
import type { RendererProps } from './types';

export function GroundingSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseGrounding(artifact.data);
  const verdicts = data?.verdicts ?? [];
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!data || (data.projects.length === 0 && verdicts.length === 0)) return fullText;
  const before = mode === 'clouds' && previous ? parseGrounding(previous.data) : null;
  const byName = (ps: GroundingProject[]) => ps.map((p) => ({ ...p, id: p.name }));
  const projectsChanged = before !== null && changedById(byName(before.projects), byName(data.projects)).size > 0;
  const changedVerdicts = before ? changedById(before.verdicts, verdicts) : new Set<string>();
  const table = (
    <table className="bp-table">
      <thead>
        <tr>
          <th>Project</th>
          <th>Checkout</th>
          <th>Verify</th>
          <th>Instructions</th>
        </tr>
      </thead>
      <tbody>
        {data.projects.map((p) => (
          <tr key={p.name}>
            <td>{p.name}</td>
            <td>{p.sha ? <code>{p.sha.slice(0, 7)}</code> : 'empty repository'}</td>
            <td>
              <VerifyCell project={p} />
            </td>
            <td>{p.instructionFile && p.instructionFile !== 'none' ? p.instructionFile : 'none'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
  return (
    <>
      {data.projects.length > 0 && (projectsChanged ? <RevisionCloud version={artifact.version}>{table}</RevisionCloud> : table)}
      <VerdictList title="Verified" items={verdicts.filter((v) => v.status === 'verified')} changed={changedVerdicts} version={artifact.version} />
      <VerdictList title="Contradicted" items={verdicts.filter((v) => v.status === 'contradicted')} changed={changedVerdicts} version={artifact.version} />
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written grounding</summary>
        {fullText}
      </details>
    </>
  );
}

function VerifyCell({ project }: { project: GroundingProject }) {
  if (project.verifyCommand) return <code>{project.verifyCommand}</code>;
  if (!project.verifyCandidate) return <>none declared</>;
  return (
    <>
      none declared; the repository documents <code>{project.verifyCandidate}</code>
    </>
  );
}

interface VerdictListProps {
  title: string;
  items: Verdict[];
  changed: Set<string>;
  version: number;
}

function VerdictList({ title, items, changed, version }: VerdictListProps) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className="bp-verdicts">
        {items.map((v) => (
          <li key={v.id}>
            {changed.has(v.id) ? <RevisionCloud version={version}>{row(v)}</RevisionCloud> : row(v)}
          </li>
        ))}
      </ul>
    </section>
  );
}

function row(v: Verdict) {
  return (
    <>
      <span className="bp-id">{v.id}</span> {withoutTags(v.evidence)} <SourceChips text={v.evidence} />
    </>
  );
}
