import type { LogMarker } from '../../types';
import { personaName, stageText } from './stages';

export function StageDivider({ marker }: { marker: LogMarker }) {
  const name = stageText(marker.stage).name;
  const persona = marker.persona ? personaName(marker.persona) : '';
  return (
    <div className="bp-stage-divider" role="separator" aria-label={persona ? `${name}, ${persona}` : name}>
      <b>{name}</b>
      {persona && <span>{persona}</span>}
    </div>
  );
}
