import { createContext, useContext } from 'react';
import type { BlueprintArtifactRef, BlueprintSlice } from '../../types';

export interface BlueprintSheetActions {
  /** Opens the sheet on a stage's slice ("" for the whole blueprint), at a version when given. */
  openArtifact: (stage: string, slice: string, version?: number) => void;
  /** The blueprint's slices, so transcript rows can name theirs. */
  slices: readonly BlueprintSlice[];
  /** The blueprint runs in plain language, so transcript rows fold technical rounds. */
  plainLanguage?: boolean;
}

export const BlueprintSheetContext = createContext<BlueprintSheetActions | null>(null);

/** The surrounding blueprint pane's sheet; null outside one. */
export function useBlueprintSheet(): BlueprintSheetActions | null {
  return useContext(BlueprintSheetContext);
}

/** The view's artifact refs, kept apart from the sheet actions so a snapshot re-renders only the rows that read reviews. */
export const BlueprintArtifactsContext = createContext<readonly BlueprintArtifactRef[]>([]);

/** The surrounding blueprint pane's artifact refs; empty outside one and while it has no view. */
export function useBlueprintArtifacts(): readonly BlueprintArtifactRef[] {
  return useContext(BlueprintArtifactsContext);
}
