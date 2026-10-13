import type { JiraEpicPreview, JiraIssueNode, ProjectConfig } from '../../types';
import { setProject, toggle, type ImportForm } from './jiraImportModel';
import { JiraIssueRow } from './JiraIssueRow';

interface JiraIssueTreeProps {
  preview: JiraEpicPreview;
  form: ImportForm;
  projects: ProjectConfig[];
  /** Sub-task key -> parent key, for sub-tasks split from their parent's project. */
  warnings: Map<string, string>;
  /** Keys whose target project's card prefix is a Jira key in this import. */
  clashes: ReadonlySet<string>;
  onChange: (form: ImportForm) => void;
}

export function JiraIssueTree({
  preview,
  form,
  projects,
  warnings,
  clashes,
  onChange,
}: JiraIssueTreeProps) {
  if (preview.children.length === 0) {
    return (
      <p className="text-sm" style={{ color: 'var(--grey1)' }}>
        This epic has no child issues.
      </p>
    );
  }

  const row = (node: JiraIssueNode) => (
    <JiraIssueRow
      node={node}
      checked={form.selected.has(node.key)}
      project={form.projects[node.key] ?? ''}
      projects={projects}
      splitFrom={warnings.get(node.key)}
      prefixClash={clashes.has(node.key)}
      onToggle={() => onChange(toggle(form, node.key))}
      onProject={(p) => onChange(setProject(form, preview, node.key, p))}
    />
  );

  return (
    <ul aria-label="Epic issues" className="flex flex-col gap-2">
      {preview.children.map((child) => (
        <li key={child.key}>
          {row(child)}
          {child.subtasks && child.subtasks.length > 0 && (
            <ul className="ml-6 mt-1 flex flex-col gap-1">
              {child.subtasks.map((st) => (
                <li key={st.key}>{row(st)}</li>
              ))}
            </ul>
          )}
        </li>
      ))}
    </ul>
  );
}
