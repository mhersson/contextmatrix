import { useAuth } from './useAuth';
import { useAgentId } from './useAgentId';

/**
 * The effective human identity for card operations, unified across auth
 * modes: multi derives human:<username> from the session (matching what the
 * server writes to assigned_agent, so ownership comparisons hold); none
 * keeps the per-browser localStorage id. Null only while logged out in
 * multi mode — unreachable behind the AuthGate.
 */
export function useIdentity(): { identity: string | null } {
  const { mode, user } = useAuth();
  const { agentId } = useAgentId(mode === 'none');

  if (mode === 'multi') {
    return { identity: user ? `human:${user.username}` : null };
  }

  return { identity: agentId };
}
