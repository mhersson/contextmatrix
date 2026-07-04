import { type ReactNode } from 'react';

/** Inline form error: --bg-red panel, --red text, warning icon, role="alert". */
export function AuthError({ children }: { children: ReactNode }) {
  return (
    <div className="auth-err" role="alert">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 8v4.5" />
        <path d="M12 16h.01" />
      </svg>
      <span>{children}</span>
    </div>
  );
}
