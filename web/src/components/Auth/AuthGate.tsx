import { type ReactNode } from 'react';
import { useLocation } from 'react-router-dom';
import { useAuth } from '../../hooks/useAuth';
import { LoginPage } from './LoginPage';
import { TokenRedemptionPage } from './TokenRedemptionPage';

/**
 * AuthGate decides what the browser shows: a minimal splash while the auth
 * mode is being resolved, the one-time-link redemption page (reachable with
 * or without a session), the login page (multi mode, no session), or the app.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const { mode, status } = useAuth();
  const location = useLocation();

  if (status === 'loading') {
    return (
      <div className="h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--bg-dim)' }}>
        <div className="text-sm" style={{ color: 'var(--grey1)' }}>
          Loading…
        </div>
      </div>
    );
  }

  // One-time links must render for logged-out AND logged-in visitors (an
  // admin verifying an invite link should not be bounced to the board).
  if (location.pathname.startsWith('/auth/token/')) {
    return <TokenRedemptionPage />;
  }

  if (mode === 'multi' && status === 'anonymous') {
    return <LoginPage />;
  }

  return <>{children}</>;
}
