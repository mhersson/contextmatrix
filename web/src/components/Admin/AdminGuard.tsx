import type { ReactNode } from 'react';
import { useAuth } from '../../hooks/useAuth';
import { NotFound } from '../NotFound';

interface AdminGuardProps {
  children: ReactNode;
}

/**
 * Gates admin-only routes (Users, Credentials, Chats, Model selection)
 * behind `user.is_admin` — but only in multi mode. In none mode there is no
 * admin role at all (single-tenant, no auth — see CLAUDE.md § Trust model),
 * so the route is open, same trust posture as project management. Non-admins
 * in multi mode get the existing NotFound page rather than a 403 — admin
 * routes shouldn't reveal their existence to non-admin accounts.
 */
export function AdminGuard({ children }: AdminGuardProps) {
  const { user, mode } = useAuth();

  if (mode === 'none') {
    return <>{children}</>;
  }

  if (!user?.is_admin) {
    return <NotFound />;
  }

  return <>{children}</>;
}
