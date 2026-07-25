import { useId } from 'react';
import { useOptionalAuth } from '../../../hooks/useAuth';
import { useUsers } from '../../../hooks/useUsers';
import { userLabel } from '../../../lib/users';
import { ChipPicker } from '../ChipPicker';

interface MetadataAssigneeProps {
  assignee: string | undefined;
  onChange: (username: string) => void;
  disabled?: boolean;
}

/**
 * Assignee section of the Info rail tab. Multi-mode only - self-hides in
 * none mode (no user roster/session concept there) and when rendered
 * outside an AuthProvider. A stale assignee (a username no longer in the
 * roster, e.g. a deactivated user) stays selectable, labeled "(unknown)",
 * so clearing or reassigning doesn't require the user to reappear first.
 */
export function MetadataAssignee({ assignee, onChange, disabled = false }: MetadataAssigneeProps) {
  // Hooks run unconditionally before the early return below (rules of hooks).
  const auth = useOptionalAuth();
  const users = useUsers(auth?.mode === 'multi');
  const selectId = useId();

  if (auth?.mode !== 'multi') return null;

  const rosterUsernames = users.map((u) => u.username);
  const value = assignee ?? '';
  const staleCurrent = value && !rosterUsernames.includes(value) ? value : null;
  const options = ['', ...rosterUsernames, ...(staleCurrent ? [staleCurrent] : [])];

  const optionLabels: Record<string, string> = { '': 'Unassigned' };
  for (const u of users) {
    optionLabels[u.username] = userLabel(u);
  }
  if (staleCurrent) {
    optionLabels[staleCurrent] = `${staleCurrent} (unknown)`;
  }

  return (
    <section className="bf-aside-section">
      <h4>Assignee</h4>
      <ChipPicker
        id={selectId}
        value={value}
        options={options}
        optionLabels={optionLabels}
        tint="var(--blue)"
        ariaLabel="Assignee"
        title={value ? (optionLabels[value] ?? value) : 'Unassigned'}
        disabled={disabled}
        onChange={onChange}
      />
    </section>
  );
}
