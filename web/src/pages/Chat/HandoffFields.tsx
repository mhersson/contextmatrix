import { useId } from 'react';
import { BLUEPRINT_MESSAGE_MAX_BYTES, formatBytes, utf8Length } from '../../components/ChatPanel/chatEntryUtils';
import { Field } from './Field';

export function HandoffBanner({ chatTitle }: { chatTitle: string }) {
  return (
    <p className="nd-banner" role="note">
      Handed over from the chat <b>{chatTitle}</b>
    </p>
  );
}

export function NeedField({ value, onChange }: { value: string; onChange: (need: string) => void }) {
  const fieldId = useId();
  const sizeId = useId();
  const bytes = utf8Length(value);
  const over = bytes > BLUEPRINT_MESSAGE_MAX_BYTES;
  return (
    <Field label="Need" htmlFor={fieldId}>
      <textarea
        id={fieldId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-describedby={sizeId}
        aria-invalid={over || undefined}
        rows={8}
        className="bf-input nd-need"
      />
      <p id={sizeId} className={`nd-note${over ? ' nd-note--over' : ''}`}>
        {formatBytes(bytes)} of {formatBytes(BLUEPRINT_MESSAGE_MAX_BYTES)}
      </p>
    </Field>
  );
}
