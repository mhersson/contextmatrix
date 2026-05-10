import { useState } from 'react';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

export function useUnsavedGuard<T>(onCommit: (target: T) => void): {
  dirty: boolean;
  setDirty: (d: boolean) => void;
  guard: (target: T) => void;
  modal: React.ReactNode;
} {
  const [dirty, setDirty] = useState(false);
  const [pending, setPending] = useState<T | null>(null);

  const guard = (target: T) => {
    if (dirty) {
      setPending(target);
      return;
    }
    onCommit(target);
  };

  const modal = (
    <ConfirmModal
      open={pending !== null}
      title="Discard unsaved changes?"
      message="You have unsaved edits. Switching docs will discard them."
      confirmLabel="Discard"
      cancelLabel="Keep editing"
      variant="danger"
      onConfirm={() => {
        if (pending !== null) onCommit(pending);
        setPending(null);
        setDirty(false);
      }}
      onCancel={() => setPending(null)}
    />
  );

  return { dirty, setDirty, guard, modal };
}
