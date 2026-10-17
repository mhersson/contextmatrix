import { BodyMarkdown } from '../BodyMarkdown';
import { parseSlices } from '../artifactData';
import type { RendererProps } from './types';

export function SlicesSheet({ artifact, previous, mode }: RendererProps) {
  const slices = parseSlices(artifact.data) ?? [];
  return (
    <>
      {slices.length > 0 && (
        <ul className="bp-rows">
          {slices.map((s) => (
            <li key={s.id} className="bp-row">
              <span className="bp-id">{s.id}</span>
              <div>
                <b>{s.title}</b>
                {s.value && <p>{s.value}</p>}
                <div className="bp-meta">
                  {s.projects.map((p) => (
                    <span key={p} className="bp-pill">
                      {p}
                    </span>
                  ))}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
    </>
  );
}
