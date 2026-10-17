import { BodyMarkdown } from '../BodyMarkdown';
import { parseReadiness } from '../artifactData';
import { latestArtifact } from '../stages';
import type { RendererProps } from './types';

// A rejected sign-off restarts the loop while versions keep rising, so only
// the newest version has a round: the pipeline step's.
export function ReadinessSheet({ artifact, previous, mode, view }: RendererProps) {
  const cards = parseReadiness(artifact.data) ?? [];
  const step = view.pipeline.find((s) => s.stage === artifact.stage);
  const newest = latestArtifact(view, artifact.stage)?.version === artifact.version;
  const fullText = <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
  const round =
    newest && step?.round && step.max_rounds ? (
      <p className="bp-sub">
        Round {step.round} of {step.max_rounds}
      </p>
    ) : null;
  if (cards.length === 0)
    return (
      <>
        {round}
        {fullText}
      </>
    );
  return (
    <>
      {round}
      <ul className="bp-readiness">
        {cards.map((c) => (
          <li key={c.id}>
            <span className="bp-id">{c.id}</span>
            {c.ready ? (
              <span className="bp-ready">Ready</span>
            ) : (
              <div>
                <span className="bp-not-ready">{c.findings.length === 1 ? 'Finding' : 'Findings'}</span>
                <ul className="bp-list">
                  {c.findings.map((f, i) => (
                    <li key={i}>{f}</li>
                  ))}
                </ul>
              </div>
            )}
          </li>
        ))}
      </ul>
      <details className="bp-fulltext" open={mode === 'compare'}>
        <summary>Written readiness</summary>
        {fullText}
      </details>
    </>
  );
}
