import { useId, useState } from 'react';

interface PasswordInputProps {
  label: string;
  hint?: string;
  value: string;
  onChange: (value: string) => void;
  autoComplete: 'current-password' | 'new-password';
  autoFocus?: boolean;
  required?: boolean;
}

/**
 * Password field with an inline show/hide toggle. Shared by LoginPage,
 * TokenRedemptionPage, and ChangePasswordModal so the reveal affordance
 * behaves identically everywhere a password is typed.
 */
export function PasswordInput({ label, hint, value, onChange, autoComplete, autoFocus, required }: PasswordInputProps) {
  const [revealed, setRevealed] = useState(false);
  const id = useId();

  return (
    <div className="auth-fld">
      <div className="auth-lab">
        <label htmlFor={id}>{label}</label>
        {hint && <span className="auth-hint">{hint}</span>}
      </div>
      <div className="relative">
        <input
          id={id}
          type={revealed ? 'text' : 'password'}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          autoComplete={autoComplete}
          autoFocus={autoFocus}
          required={required}
          spellCheck={false}
          className="auth-input auth-input--pw"
        />
        <button
          type="button"
          className="auth-eye"
          aria-label={revealed ? 'Hide password' : 'Show password'}
          aria-pressed={revealed}
          onClick={() => setRevealed((v) => !v)}
        >
          {revealed ? <EyeOffIcon /> : <EyeIcon />}
        </button>
      </div>
    </div>
  );
}

function EyeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M2.06 12.35a1 1 0 0 1 0-.7C3.42 8.1 7.36 5 12 5s8.58 3.1 9.94 6.65a1 1 0 0 1 0 .7C20.58 15.9 16.64 19 12 19s-8.58-3.1-9.94-6.65Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function EyeOffIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M10.73 5.08A10 10 0 0 1 12 5c4.64 0 8.58 3.1 9.94 6.65a1 1 0 0 1 0 .7 13.4 13.4 0 0 1-1.67 2.7" />
      <path d="M6.61 6.61A13.5 13.5 0 0 0 2.06 11.65a1 1 0 0 0 0 .7C3.42 15.9 7.36 19 12 19c1.31 0 2.57-.25 3.73-.7" />
      <path d="M9.88 9.88a3 3 0 0 0 4.24 4.24" />
      <path d="m3 3 18 18" />
    </svg>
  );
}
