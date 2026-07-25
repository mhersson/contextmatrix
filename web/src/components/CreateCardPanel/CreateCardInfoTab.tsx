import type { Card, ProjectConfig } from '../../types';
import { MetadataSkills } from '../CardPanel/metadata/MetadataSkills';
import { MetadataAssignee } from '../CardPanel/metadata/MetadataAssignee';
import { chipTint, stateColors } from '../../lib/chip';
import { ParentSearch } from './ParentSearch';

interface CreateCardInfoTabProps {
  config: ProjectConfig;
  cards: Card[];
  assignee: string;
  onAssigneeChange: (v: string) => void;
  parent: string;
  onSetParent: (id: string) => void;
  skills: string[] | null;
  onSkillsChange: (next: string[] | null) => void;
}

/**
 * Info tab of the create-card panel. Mirrors the Card Details Info tab's
 * section order (Assignee, Agent, Status, related, Skills), with the
 * not-yet-applicable sections replaced by static placeholders.
 */
export function CreateCardInfoTab({
  config,
  cards,
  assignee,
  onAssigneeChange,
  parent,
  onSetParent,
  skills,
  onSkillsChange,
}: CreateCardInfoTabProps) {
  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <MetadataAssignee assignee={assignee || undefined} onChange={onAssigneeChange} />

      <section className="bf-aside-section">
        <h4>Agent</h4>
        <div className="bf-spread">
          <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>no agent yet</span>
          <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>assigned on create</span>
        </div>
      </section>

      <section className="bf-aside-section">
        <h4>Initial state</h4>
        <div className="text-xs flex items-center gap-2" style={{ color: 'var(--grey1)' }}>
          <span>Cards are created in</span>
          <span className="chip-pill" style={chipTint(stateColors[config.states[0]] || 'var(--grey1)')}>
            {config.states[0]}
          </span>
        </div>
      </section>

      <section className="bf-aside-section">
        <h4>Parent (optional)</h4>
        <ParentSearch parent={parent} setParent={onSetParent} cards={cards} />
        <div className="font-mono mt-2" style={{ color: 'var(--grey1)', fontSize: '11px', lineHeight: 1.45 }}>
          Leave empty for a top-level card. Setting a parent locks the type to <code style={{ color: 'var(--purple)' }}>subtask</code>.
        </div>
      </section>

      <MetadataSkills
        value={skills}
        config={config}
        onSkillsChange={onSkillsChange}
      />
    </div>
  );
}
