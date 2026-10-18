import type { LogMarker } from '../../types';
import { useBlueprintSheet } from './sheetContext';
import { personaName, sliceOf, sliceSuffix, stageText } from './stages';

export function StageDivider({ marker }: { marker: LogMarker }) {
  const slices = useBlueprintSheet()?.slices ?? [];
  const name = stageText(marker.stage).name + sliceSuffix(slices, sliceOf(marker));
  const persona = marker.persona ? personaName(marker.persona) : '';
  return (
    <div className="bp-stage-divider" role="separator" aria-label={persona ? `${name}, ${persona}` : name}>
      <b>{name}</b>
      {persona && <span>{persona}</span>}
    </div>
  );
}
