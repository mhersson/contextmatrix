import type { Card } from '../../../types';
import { DangerZoneTab } from '../CardPanelDangerZone';

interface DangerTabProps {
  card: Card;
  canDelete: boolean;
  deleteTooltip: string;
  isDeleting: boolean;
  onDelete: () => Promise<void>;
}

/**
 * Danger rail tab — thin adapter around `DangerZoneTab`. Kept as a peer
 * of the other tab-content components so the tab registry is uniform.
 */
export function DangerTab(props: DangerTabProps) {
  return <DangerZoneTab {...props} />;
}
