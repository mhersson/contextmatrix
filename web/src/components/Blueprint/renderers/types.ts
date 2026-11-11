import type { BlueprintArtifact, BlueprintView } from '../../../types';
import type { RevisionMode } from '../BodyMarkdown';

export interface RendererProps {
  /** The blueprint session; the board sheet reads its cost basis from CM's session record. */
  chatId?: string;
  artifact: BlueprintArtifact;
  /** The previous version; given for clouds and compare, null otherwise. */
  previous: BlueprintArtifact | null;
  mode: RevisionMode;
  view: BlueprintView;
}
