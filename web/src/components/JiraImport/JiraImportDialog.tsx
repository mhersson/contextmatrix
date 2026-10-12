import { useEffect, useId, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { api, isAPIError } from '../../api/client';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { errorMessage } from '../../lib/errors';
import { buildRequest, canSubmit } from './jiraImportModel';
import { JiraImportForm } from './JiraImportForm';
import { JiraImportResults } from './JiraImportResults';
import { useJiraEpicPreview } from './useJiraEpicPreview';
import { useJiraImportRun } from './useJiraImportRun';

interface JiraImportDialogProps {
  epicKey: string;
  /** What was typed in the form; empty falls back to the epic summary. */
  description: string;
  boardsRepo?: string;
  onClose: () => void;
}

export function JiraImportDialog({ epicKey, description, boardsRepo, onClose }: JiraImportDialogProps) {
  const { preview, form, setForm, error: loadError } = useJiraEpicPreview(epicKey);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [runConflict, setRunConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [started, setStarted] = useState<{ id: string; keys: string[] } | null>(null);
  const run = useJiraImportRun(started?.id ?? null, started?.keys.length ?? 0);
  const navigate = useNavigate();
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const error = loadError ?? submitError;
  const runActive = !!preview?.playbook?.run_active || runConflict;

  // The surface takes focus: nothing inside is focusable until the preview loads.
  useFocusTrap(dialogRef, true, dialogRef);

  // Capture phase and preventDefault, so page Escape handlers that respect defaultPrevented stay quiet.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !busy) {
        e.preventDefault();
        onClose();
      }
    }
    window.addEventListener('keydown', onKey, { capture: true });
    return () => window.removeEventListener('keydown', onKey, { capture: true });
  }, [busy, onClose]);

  const submit = async () => {
    if (!preview || !form || busy || runActive || !canSubmit(preview, form)) return;
    setBusy(true);
    setSubmitError(null);
    try {
      const req = buildRequest(preview, form, description, boardsRepo);
      const { import_id } = await api.importJiraEpic(epicKey, req);
      setStarted({ id: import_id, keys: req.selections.map((s) => s.key) });
    } catch (err) {
      if (isAPIError(err) && err.code === 'JIRA_PLAYBOOK_RUNNING') setRunConflict(true);
      else setSubmitError(errorMessage(err, 'Import failed'));
    } finally {
      setBusy(false);
      // The busy Import button disables itself, and on success the results view removes it.
      dialogRef.current?.focus();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
      <div className="absolute inset-0 bg-black/50" aria-hidden="true" onClick={busy ? undefined : onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="relative z-10 outline-none w-full max-w-3xl max-h-[85vh] rounded-lg p-5 border flex flex-col gap-4"
        style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
      >
        <h2 id={titleId} className="text-base font-semibold truncate" style={{ color: 'var(--fg)' }}>
          Import Jira epic {epicKey}
          {preview ? ` · ${preview.epic.summary}` : ''}
        </h2>
        {error && (
          <div role="alert" className="text-sm" style={{ color: 'var(--red)' }}>
            {error}
          </div>
        )}
        {!preview && !error && (
          <p className="text-sm" style={{ color: 'var(--grey1)' }}>
            Loading the epic from Jira…
          </p>
        )}
        {run && started ? (
          <JiraImportResults
            run={run}
            keys={started.keys}
            onOpen={() => run.playbookId && navigate(`/playbooks/${encodeURIComponent(run.playbookId)}`)}
            onClose={onClose}
          />
        ) : (
          preview &&
          form && (
            <JiraImportForm
              preview={preview}
              form={form}
              onChange={setForm}
              runActive={runActive}
              busy={busy}
              onSubmit={submit}
              onCancel={onClose}
            />
          )
        )}
      </div>
    </div>
  );
}
