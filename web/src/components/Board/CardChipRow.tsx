import type { Card } from '../../types';
import { gitHubIcon } from '../icons';
import { chipTint, typeColors } from '../../lib/chip';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useUsers } from '../../hooks/useUsers';
import { userInitials, userLabel } from '../../lib/users';

export interface CardChipRowProps {
  card: Card;
  compact?: boolean;
}

/**
 * Renders the chip row for a board card.
 *
 * compact=true  - collapsed card header: type initial and source badges.
 * compact=false - expanded card footer: the assignee initials circle only,
 *                 so an assigned card is spottable at a glance. Renders
 *                 nothing when unassigned - every other signal lives in the
 *                 header icon cluster (CardSignalIcons) or the open card
 *                 panel.
 */
export function CardChipRow({ card, compact = false }: CardChipRowProps) {
  // Called unconditionally before the compact early return (rules of hooks).
  const auth = useOptionalAuth();
  const users = useUsers(auth?.mode === 'multi' && !!card.assignee);

  if (compact) {
    return (
      <>
        {card.type !== 'subtask' && (
          <span
            className="chip-pill flex-shrink-0"
            style={chipTint(typeColors[card.type] || 'var(--grey1)')}
            title={card.type}
            aria-label={`Type: ${card.type}`}
          >
            {card.type.charAt(0)}
          </span>
        )}
        {card.source?.system === 'github' && gitHubIcon}
        {card.source && !card.vetted && (
          <span className="chip-pill flex-shrink-0" style={chipTint('var(--yellow)')}>
            unvetted
          </span>
        )}
      </>
    );
  }

  // Assignee circle - hidden outside multi mode (no logins, no ownership
  // semantics to display) even if a hand-edited board file sets one.
  const assignee = card.assignee;
  if (auth?.mode !== 'multi' || !assignee) return null;

  const rosterUser = users.find((u) => u.username === assignee);
  const label = rosterUser ? userLabel(rosterUser) : assignee;

  return (
    <div className="flex items-center flex-wrap gap-2">
      <span
        className="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-semibold flex-shrink-0"
        style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--blue)' }}
        title={`Assignee: ${label}`}
        role="img"
        aria-label={`Assignee: ${label}`}
      >
        {userInitials(rosterUser?.display_name, assignee)}
      </span>
    </div>
  );
}
