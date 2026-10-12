import { useId } from 'react';
import { useProjects } from '../../hooks/useProjects';
import type { JiraEpicPreview } from '../../types';
import { canSubmit, prefixClashes, splitWarnings, type ImportForm } from './jiraImportModel';
import { JiraImportOptions } from './JiraImportOptions';
import { JiraIssueTree } from './JiraIssueTree';

interface JiraImportFormProps {
  preview: JiraEpicPreview;
  form: ImportForm;
  onChange: (form: ImportForm) => void;
  /** The epic's playbook has an active run, from the preview or a refused import. */
  runActive: boolean;
  busy: boolean;
  onSubmit: () => void;
  onCancel: () => void;
}

function RunActiveNotice() {
  const id = useId();
  return (
    <div role="alert" aria-labelledby={id} className="text-sm" style={{ color: 'var(--yellow)' }}>
      <strong id={id}>Playbook run in progress.</strong> The epic&apos;s playbook has an active run. Stop
      the run or wait for it to finish, then open this import again.
    </div>
  );
}

export function JiraImportForm({ preview, form, onChange, runActive, busy, onSubmit, onCancel }: JiraImportFormProps) {
  const { projects } = useProjects();
  const count = form.selected.size;

  return (
    <>
      {runActive && <RunActiveNotice />}
      <div className="overflow-y-auto min-h-0 flex-1">
        <JiraIssueTree
          preview={preview}
          form={form}
          projects={projects}
          warnings={splitWarnings(preview, form)}
          clashes={prefixClashes(preview, form, projects)}
          onChange={onChange}
        />
      </div>
      <JiraImportOptions preview={preview} form={form} projects={projects} onChange={onChange} />
      <div className="flex gap-2 justify-end">
        <button type="button" onClick={onCancel} disabled={busy} className="bf-btn-ghost bf-btn-sm">
          Cancel
        </button>
        <button
          type="button"
          onClick={onSubmit}
          disabled={busy || runActive || !canSubmit(preview, form)}
          className="bf-btn-primary"
        >
          {busy ? 'Importing…' : `Import ${count} issue${count === 1 ? '' : 's'}`}
        </button>
      </div>
    </>
  );
}
