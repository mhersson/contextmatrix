import type { ReactNode } from 'react';
import type { BlueprintCurrentRun, BlueprintView } from '../../types';
import type { RevisionMode } from './BodyMarkdown';
import { DraftBody, ProgressLine, RefusedBand } from './DraftBody';
import { Stamp } from './Stamp';
import { loadArtifact, useBlueprintArtifact } from './artifactCache';
import type { HeldDraft } from './draft';
import { ArtifactRenderer } from './renderers/ArtifactRenderer';
import { stampFor } from './sheetModel';
import { artifactsFor, stageText } from './stages';

interface PaperProps {
  chatId: string;
  view: BlueprintView;
  stage: string;
  /** A stored version, or the version a run is writing. */
  version: number;
  /** The run writing the shown version; null when the version is stored. */
  writing: BlueprintCurrentRun | null;
  /** The shown version's streaming draft, if any. */
  draft: HeldDraft | null;
  compare: boolean;
}

export function Paper({ chatId, view, stage, version, writing, draft, compare }: PaperProps) {
  const refs = artifactsFor(view, stage);
  const stored = refs.find((r) => r.version === version) ?? null;
  const previousRef = refs.filter((r) => r.version < version).pop() ?? null;
  const isNewest = stored !== null && stored.version === refs[refs.length - 1]?.version;
  const wantPrevious = previousRef !== null && (stored === null || compare || isNewest);
  const entry = useBlueprintArtifact(chatId, stage, stored ? version : null);
  const previousEntry = useBlueprintArtifact(chatId, stage, wantPrevious && previousRef ? previousRef.version : null);
  const previous = previousEntry?.status === 'ready' ? previousEntry.artifact : null;
  const stamp = stored ? stampFor(view, stored) : null;
  const mode: RevisionMode = !previous ? 'plain' : compare ? 'compare' : isNewest ? 'clouds' : 'plain';
  const title = stageText(stage).title;

  let content: ReactNode;
  if (stored === null) {
    content =
      draft && draft.text !== '' ? (
        <DraftBody draft={draft} />
      ) : (
        <>
          {writing && <ProgressLine run={writing} stage={stage} />}
          {draft?.refused && <RefusedBand reason={draft.reason} />}
          {previous && (
            <div className="bp-dimmed">
              <ArtifactRenderer artifact={previous} previous={null} mode="plain" view={view} />
            </div>
          )}
        </>
      );
  } else if (entry?.status === 'ready') {
    const problems = entry.artifact.check_problems ?? [];
    content = (
      <>
        {problems.length > 0 && (
          <div className="bp-check-problems" role="note" aria-label="Check problems">
            <b>Accepted with check problems</b>
            <ul>
              {problems.map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          </div>
        )}
        <ArtifactRenderer artifact={entry.artifact} previous={mode === 'plain' ? null : previous} mode={mode} view={view} />
      </>
    );
  } else if (entry?.status === 'error') {
    content = (
      <p className="bp-load-error" role="alert">
        Couldn&apos;t load this sheet.{' '}
        <button type="button" className="bf-btn-ghost bf-btn-sm" onClick={() => loadArtifact(chatId, stage, version)}>
          Retry
        </button>
      </p>
    );
  } else {
    content = <p className="bp-sub">Loading…</p>;
  }

  return (
    <article className="bp-paper" aria-label={`${title} rev ${version}`}>
      {stamp && <Stamp stamp={stamp} />}
      <h1>{title}</h1>
      {content}
    </article>
  );
}
