import { useEffect, useId, useRef, useState } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import type { InviteInfo } from '../../types';

interface InviteLinkDialogProps {
  open: boolean;
  token: string;
  purpose: InviteInfo['purpose'];
  onClose: () => void;
}

/**
 * Shows a one-time invite/reset link for copying. Reused by both the
 * create-user flow and the per-row "New link" (regenerate) action — both
 * just hand this component a fresh token + purpose.
 */
export function InviteLinkDialog({ open, token, purpose, onClose }: InviteLinkDialogProps) {
  const [copied, setCopied] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useFocusTrap(dialogRef, open);

  // Fresh "Copied!" state every time the dialog reopens (render-time
  // pattern — see useBoard.ts / web/CLAUDE.md § rail sync).
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setCopied(false);
  }

  useEffect(() => {
    if (!open) return;

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const link = `${window.location.origin}/auth/token/${token}`;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API unavailable/denied — the link text is still selectable.
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={onClose} />

      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-[28rem] rounded-lg p-5 border flex flex-col gap-4"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId} className="text-base font-semibold" style={{ color: 'var(--fg)' }}>
          {purpose === 'reset' ? 'Password reset link' : 'Invite link'}
        </h2>

        <p className="text-sm" style={{ color: 'var(--grey1)' }}>
          Share this one-time link with the user — it expires in 48 hours and can only be used once.
        </p>

        <div
          className="rounded px-2 py-1.5 border font-mono text-xs break-all"
          style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)', color: 'var(--fg)' }}
        >
          {link}
        </div>

        <div className="flex gap-2 justify-end">
          <button
            type="button"
            onClick={() => void copy()}
            className="rounded py-1.5 px-4 font-medium"
            style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}
          >
            {copied ? 'Copied!' : 'Copy link'}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded py-1.5 px-4"
            style={{ backgroundColor: 'var(--bg3)', color: 'var(--fg)' }}
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
