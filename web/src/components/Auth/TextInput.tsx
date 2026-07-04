import { useId } from 'react';

interface TextInputProps {
  label: string;
  hint?: string;
  value: string;
  onChange: (value: string) => void;
  autoComplete?: string;
  autoFocus?: boolean;
  required?: boolean;
}

/** Text field in the auth style: label row with optional mono micro-hint. */
export function TextInput({ label, hint, value, onChange, autoComplete, autoFocus, required }: TextInputProps) {
  const id = useId();

  return (
    <div className="auth-fld">
      <div className="auth-lab">
        <label htmlFor={id}>{label}</label>
        {hint && <span className="auth-hint">{hint}</span>}
      </div>
      <input
        id={id}
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        required={required}
        spellCheck={false}
        className="auth-input"
      />
    </div>
  );
}
