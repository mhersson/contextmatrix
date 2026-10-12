import { useId } from 'react';
import type { JiraEpicPreview, ProjectConfig } from '../../types';
import { setAllProjects, type ImportForm } from './jiraImportModel';

interface JiraImportOptionsProps {
  preview: JiraEpicPreview;
  form: ImportForm;
  projects: ProjectConfig[];
  onChange: (form: ImportForm) => void;
}

export function JiraImportOptions({ preview, form, projects, onChange }: JiraImportOptionsProps) {
  const titleId = useId();
  const existing = preview.playbook;
  const epicText = preview.epic.description;

  return (
    <div className="flex flex-col gap-3 text-sm" style={{ color: 'var(--grey2)' }}>
      <label className="flex items-center gap-2">
        Project for all issues
        <select
          aria-label="Project for all issues"
          value=""
          onChange={(e) => e.target.value && onChange(setAllProjects(form, preview, e.target.value))}
          className="bf-input text-xs"
          style={{ width: 'auto', minWidth: '11rem' }}
        >
          <option value="">Choose…</option>
          {projects.map((p) => (
            <option key={p.name} value={p.name}>
              {p.display_name || p.name}
            </option>
          ))}
        </select>
      </label>

      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={form.vetted}
          onChange={(e) => onChange({ ...form, vetted: e.target.checked })}
          className="accent-[var(--green)]"
        />
        Mark imported cards vetted
      </label>

      <div>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={form.includeEpic && !!epicText}
            disabled={!epicText}
            onChange={(e) => onChange({ ...form, includeEpic: e.target.checked })}
            className="accent-[var(--green)]"
          />
          Include epic description in each card
        </label>
        {epicText ? (
          <details className="ml-6 mt-1">
            <summary className="cursor-pointer text-xs">Epic description</summary>
            <pre className="whitespace-pre-wrap text-xs mt-1 max-h-40 overflow-y-auto" style={{ color: 'var(--fg)' }}>
              {epicText}
            </pre>
          </details>
        ) : (
          <p className="ml-6 text-xs" style={{ color: 'var(--grey1)' }}>The epic has no description.</p>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor={titleId}>Playbook title</label>
        <input
          id={titleId}
          value={form.title}
          readOnly={!!existing}
          onChange={(e) => onChange({ ...form, title: e.target.value })}
          className="bf-input"
        />
        {existing && (
          <p className="text-xs" style={{ color: 'var(--aqua)' }}>Adds to the existing playbook for this epic.</p>
        )}
      </div>
    </div>
  );
}
