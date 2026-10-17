import type { LogMarker } from '../../types';
import { SheetIcon } from './icons';
import { useBlueprintSheet } from './sheetContext';
import { stageText } from './stages';

/** A stored artifact in the transcript; inside a blueprint pane a click opens that version on the sheet. */
export function SheetReference({ marker }: { marker: LogMarker }) {
  const sheet = useBlueprintSheet();
  const content = (
    <>
      <SheetIcon />
      <span className="bp-ref-text">
        <b>{stageText(marker.stage).tab}</b>
        {marker.version && <> <span>rev {marker.version}</span></>}
        {marker.note && <span className="bp-ref-note">{marker.note}</span>}
      </span>
    </>
  );
  if (!sheet) {
    return (
      <div className="bp-ref" data-testid="sheet-reference">
        {content}
      </div>
    );
  }
  return (
    <button
      type="button"
      className="bp-ref bp-ref--link"
      data-testid="sheet-reference"
      onClick={() => sheet.openArtifact(marker.stage, marker.version)}
    >
      {content}
    </button>
  );
}
