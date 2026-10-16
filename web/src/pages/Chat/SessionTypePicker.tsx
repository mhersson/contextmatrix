interface SessionTypePickerProps {
  blueprint: boolean;
  onChange: (blueprint: boolean) => void;
}

export function SessionTypePicker({ blueprint, onChange }: SessionTypePickerProps) {
  return (
    <fieldset className="flex gap-4 mb-3 text-xs" style={{ color: 'var(--fg)' }}>
      <legend className="sr-only">Session type</legend>
      <label className="flex items-center gap-1">
        <input type="radio" name="session-type" checked={!blueprint} onChange={() => onChange(false)} />
        Chat
      </label>
      <label className="flex items-center gap-1">
        <input type="radio" name="session-type" checked={blueprint} onChange={() => onChange(true)} />
        Blueprint
      </label>
    </fieldset>
  );
}
