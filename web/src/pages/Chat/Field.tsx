import type { ReactNode } from 'react';

interface FieldProps {
  label: string;
  hint?: string;
  /** The control a plain label names; groups name themselves. */
  htmlFor?: string;
  children: ReactNode;
}

export function Field({ label, hint, htmlFor, children }: FieldProps) {
  return (
    <div className="nd-field">
      <div className="nd-label">
        {htmlFor ? <label htmlFor={htmlFor}>{label}</label> : <span>{label}</span>}
        {hint && <small>{hint}</small>}
      </div>
      {children}
    </div>
  );
}
