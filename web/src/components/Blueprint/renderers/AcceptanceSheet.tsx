import { BodyMarkdown } from '../BodyMarkdown';
import { parseAcceptance, type Criterion } from '../artifactData';
import { ManualChecks } from './parts';
import type { RendererProps } from './types';

export function AcceptanceSheet({ artifact, previous, mode }: RendererProps) {
  const slices = parseAcceptance(artifact.data);
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  if (!slices || slices.length === 0) return fullText;
  const manual = slices.flatMap((s) => s.manual);
  return (
    <>
      {slices.map((s) => (
        <section key={s.id}>
          <h3>{s.id}</h3>
          {byProject(s.criteria).map(([project, criteria]) => (
            <div key={project}>
              {project && <h4>{project}</h4>}
              <ul className="bp-criteria">
                {criteria.map((c) => (
                  <li key={c.id}>
                    <span className="bp-id">{c.id}</span> {c.text}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
      ))}
      <ManualChecks title="Manual checks" items={manual} />
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written criteria</summary>
        {fullText}
      </details>
    </>
  );
}

function byProject(criteria: Criterion[]): [string, Criterion[]][] {
  const groups = new Map<string, Criterion[]>();
  for (const c of criteria) groups.set(c.project, [...(groups.get(c.project) ?? []), c]);
  return [...groups.entries()];
}
