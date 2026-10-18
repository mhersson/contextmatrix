import { createContext, useContext } from 'react';

export interface BlueprintSheetActions {
  /** Opens the sheet on a stage's slice ("" for the whole blueprint), at a version when given. */
  openArtifact: (stage: string, slice: string, version?: number) => void;
}

export const BlueprintSheetContext = createContext<BlueprintSheetActions | null>(null);

/** The surrounding blueprint pane's sheet; null outside one. */
export function useBlueprintSheet(): BlueprintSheetActions | null {
  return useContext(BlueprintSheetContext);
}
