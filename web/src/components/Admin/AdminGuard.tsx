import type { ReactNode } from 'react';
import { useAuth } from '../../hooks/useAuth';
import { NotFound } from '../NotFound';

interface AdminGuardProps {
  children: ReactNode;
}

/**
 * Gates admin-only routes (Users, Credentials) behind `user.is_admin`.
 * Non-admins get the existing NotFound page rather than a 403 — admin
 * routes shouldn't reveal their existence to non-admin accounts.
 */
export function AdminGuard({ children }: AdminGuardProps) {
  const { user } = useAuth();

  if (!user?.is_admin) {
    return <NotFound />;
  }

  return <>{children}</>;
}
