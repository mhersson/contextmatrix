import type { Card, ProjectConfig } from '../../types';
import { MetadataStatus } from './metadata/MetadataStatus';
import { MetadataAgent } from './metadata/MetadataAgent';
import { MetadataRelated } from './metadata/MetadataRelated';
import { MetadataSource } from './metadata/MetadataSource';
import { MetadataSkills } from './metadata/MetadataSkills';
import { MetadataUsage } from './metadata/MetadataUsage';

interface CardPanelMetadataProps {
  card: Card;
  editedCard: Card;
  config: ProjectConfig;
  currentAgentId: string | null;
  workerAttached: boolean;
  onStateChange: (state: string) => void;
  onSubtaskClick: (cardId: string) => void;
  onClaim: () => void;
  onRelease: () => void;
  editedVetted: boolean;
  onVettedChange: (value: boolean) => void;
  onSkillsChange: (next: string[] | null) => void;
  excludeStateFromPicker?: string | null;
}

/**
 * Info rail tab - mirrors the design mock's `renderBifoldTab` info branch
 * (`/tmp/card-panel-explorer.html:2188-2224`). Stacked sections in four
 * peer files under `./metadata/`:
 *
 *   1. MetadataStatus  - state picker + hint + worker-status badge
 *   2. MetadataAgent   - claim/release (with ConfirmModal)
 *   3. MetadataRelated - Parent / Subtasks / Depends-on (shares hydration)
 *   4. MetadataSource  - external-link pill + vetted checkbox
 *
 * This wrapper just composes them and renders the Created/Updated footer.
 */
export function CardPanelMetadata({
  card,
  editedCard,
  config,
  currentAgentId,
  workerAttached,
  onStateChange,
  onSubtaskClick,
  onClaim,
  onRelease,
  editedVetted,
  onVettedChange,
  onSkillsChange,
  excludeStateFromPicker,
}: CardPanelMetadataProps) {
  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <MetadataStatus
        card={card}
        editedCard={editedCard}
        config={config}
        workerAttached={workerAttached}
        onStateChange={onStateChange}
        excludeStateFromPicker={excludeStateFromPicker}
      />

      <MetadataAgent
        card={card}
        currentAgentId={currentAgentId}
        workerAttached={workerAttached}
        onClaim={onClaim}
        onRelease={onRelease}
      />

      <MetadataSkills
        value={editedCard.skills}
        config={config}
        onSkillsChange={onSkillsChange}
        disabled={workerAttached || card.state !== 'todo'}
        lockedReason={
          workerAttached
            ? 'Skills locked during remote run'
            : `Skills can only be edited in todo · current state: ${card.state.replace(/_/g, ' ')}`
        }
      />

      <MetadataRelated
        card={card}
        workerAttached={workerAttached}
        onSubtaskClick={onSubtaskClick}
      />

      <MetadataSource
        card={card}
        editedVetted={editedVetted}
        onVettedChange={onVettedChange}
      />

      <MetadataUsage card={card} />

      {/* Metadata footer */}
      <section className="bf-aside-section">
        <div className="text-xs text-[var(--grey0)]">
          <div>Created: {new Date(card.created).toLocaleString()}</div>
          <div>Updated: {new Date(card.updated).toLocaleString()}</div>
        </div>
      </section>
    </div>
  );
}
