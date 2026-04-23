import { useEffect, useId, useRef, type ReactNode } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';

export interface ConfirmModalProps {
  open: boolean;
  title: string;
  message: string | ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: 'default' | 'danger';
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmModal({
  open,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  variant = 'default',
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const messageId = useId();

  // Focus trap with Confirm button as initial focus target
  useFocusTrap(dialogRef, open, confirmButtonRef);

  // Escape key closes modal
  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onCancel();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, onCancel]);

  if (!open) return null;

  const confirmStyle =
    variant === 'danger'
      ? { background: 'var(--bg-red)', color: 'var(--red)' }
      : { background: 'var(--bg-blue)', color: 'var(--aqua)' };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        aria-hidden="true"
        onClick={onCancel}
      />

      {/* Dialog panel */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={messageId}
        className="relative z-10 rounded-lg shadow-lg max-w-sm w-full mx-4 p-6 border"
        style={{
          background: 'var(--bg1)',
          borderColor: 'var(--bg3)',
          color: 'var(--fg)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2
          id={titleId}
          className="text-sm font-semibold mb-3"
          style={{ color: 'var(--fg)' }}
        >
          {title}
        </h2>

        <p
          id={messageId}
          className="text-sm mb-5"
          style={{ color: 'var(--grey1)' }}
        >
          {message}
        </p>

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 rounded text-sm transition-opacity hover:opacity-80"
            style={{ background: 'var(--bg3)', color: 'var(--fg)' }}
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmButtonRef}
            type="button"
            onClick={onConfirm}
            className="px-3 py-1.5 rounded text-sm transition-opacity hover:opacity-80 focus:outline-none focus:ring-2"
            style={{
              ...confirmStyle,
              // ring color matches accent
              ['--tw-ring-color' as string]:
                variant === 'danger' ? 'var(--red)' : 'var(--aqua)',
            }}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
