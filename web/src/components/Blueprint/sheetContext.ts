import { createContext, useContext } from 'react';

export interface BlueprintSheetActions {
  /** Opens the sheet on a stage, at a version when given. */
  openArtifact: (stage: string, version?: number) => void;
}

export const BlueprintSheetContext = createContext<BlueprintSheetActions | null>(null);

/** The surrounding blueprint pane's sheet; null outside one. */
export function useBlueprintSheet(): BlueprintSheetActions | null {
  return useContext(BlueprintSheetContext);
}
