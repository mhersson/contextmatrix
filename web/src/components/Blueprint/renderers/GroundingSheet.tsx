import { BodyMarkdown } from '../BodyMarkdown';
import { SourceChips } from '../Chips';
import { parseGrounding, withoutTags, type GroundingProject, type Verdict } from '../artifactData';
import type { RendererProps } from './types';

export function GroundingSheet({ artifact, previous, mode }: RendererProps) {
  const data = parseGrounding(artifact.data);
  const verdicts = data?.verdicts ?? [];
  return (
    <>
      {data && data.projects.length > 0 && (
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
      )}
      <VerdictList title="Verified" items={verdicts.filter((v) => v.status === 'verified')} />
      <VerdictList title="Contradicted" items={verdicts.filter((v) => v.status === 'contradicted')} />
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
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

function VerdictList({ title, items }: { title: string; items: Verdict[] }) {
  if (items.length === 0) return null;
  return (
    <section>
      <h3>{title}</h3>
      <ul className="bp-verdicts">
        {items.map((v) => (
          <li key={v.id}>
            <span className="bp-id">{v.id}</span> {withoutTags(v.evidence)} <SourceChips text={v.evidence} />
          </li>
        ))}
      </ul>
    </section>
  );
}
