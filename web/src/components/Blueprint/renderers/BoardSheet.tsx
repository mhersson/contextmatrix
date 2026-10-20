import { BodyMarkdown } from '../BodyMarkdown';
import { httpUrl, parseBoard } from '../artifactData';
import { money } from '../stages';
import { Callout, TextList } from './parts';
import type { RendererProps } from './types';

export function BoardSheet({ artifact }: RendererProps) {
  const data = parseBoard(artifact.data);
  if (!data) return <BodyMarkdown body={artifact.body} mode="plain" version={artifact.version} />;
  const compares = data.repos.flatMap((r) => {
    const url = httpUrl(r.compareUrl);
    return url ? [{ project: r.project, url }] : [];
  });
  return (
    <>
      <p className="bp-sub">
        {data.playbook ? `Playbook ${data.playbook}` : data.cards.length === 1 ? 'One card' : `${data.cards.length} cards`}
      </p>
      {data.cards.map((c) => {
        const pr = httpUrl(c.pr);
        const id = c.card || c.draft;
        return (
          <article key={id} className="bp-card" aria-label={`${id} ${c.title}`}>
            <div className="bp-card-title">
              <span className="bp-id">{id}</span>
              {c.title}
            </div>
            <div className="bp-meta">
              {c.project && <span className="bp-pill">{c.project}</span>}
              {c.card && c.draft && <span>from {c.draft}</span>}
              <span className="bp-pill">{c.state ? c.state.replace(/_/g, ' ') : 'not seen yet'}</span>
              {pr && (
                <a href={pr} target="_blank" rel="noreferrer noopener">
                  Pull request
                </a>
              )}
            </div>
          </article>
        );
      })}
      {compares.length > 0 && (
        <section>
          <h3>Compare</h3>
          <ul className="bp-list">
            {compares.map((c) => (
              <li key={c.project}>
                <a href={c.url} target="_blank" rel="noreferrer noopener">
                  {c.project}
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
      <TextList title="Manual checks after the run" items={data.manual} />
      <Callout tone="steps" title="Setup steps" items={data.steps} />
      <Callout tone="notices" title="Notices" items={data.notices} />
      {data.costUsd !== null && (
        <p className="bp-sub">
          The architect&apos;s runs cost {money(data.costUsd)}
          {data.priced ? '' : ', some runs unpriced'}.
        </p>
      )}
    </>
  );
}
