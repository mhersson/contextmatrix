import type { BlueprintArtifact, BlueprintView } from '../../../types';
import type { RevisionMode } from '../BodyMarkdown';

export interface RendererProps {
  artifact: BlueprintArtifact;
  /** The previous version; given for clouds and compare, null otherwise. */
  previous: BlueprintArtifact | null;
  mode: RevisionMode;
  view: BlueprintView;
}
