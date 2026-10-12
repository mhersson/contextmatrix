import type { JiraEpicPreview, JiraImportRequest, JiraIssueNode, ProjectConfig } from '../../types';

export interface ImportForm {
  selected: ReadonlySet<string>;
  projects: Readonly<Record<string, string>>;
  vetted: boolean;
  includeEpic: boolean;
  title: string;
}

/** Every child, each followed by its sub-tasks. */
export function flatten(preview: JiraEpicPreview): JiraIssueNode[] {
  return preview.children.flatMap((c) => [c, ...(c.subtasks ?? [])]);
}

/** An imported card can be picked again only to put it back into the playbook. */
export function isSelectable(node: JiraIssueNode): boolean {
  return !node.imported || !node.imported.in_playbook;
}

export function initialForm(preview: JiraEpicPreview): ImportForm {
  const selected = new Set<string>();
  const projects: Record<string, string> = {};
  for (const child of preview.children) {
    projects[child.key] = child.imported?.project ?? child.default_project ?? '';
    if (!child.imported && child.status_category !== 'done') selected.add(child.key);
    for (const st of child.subtasks ?? []) {
      projects[st.key] = st.imported?.project ?? st.default_project ?? projects[child.key];
      if (!st.imported && st.status_category !== 'done') selected.add(st.key);
    }
  }
  return {
    selected,
    projects,
    vetted: false,
    includeEpic: true,
    title: preview.playbook?.title ?? `${preview.epic.key} ${preview.epic.summary}`,
  };
}

export function toggle(form: ImportForm, key: string): ImportForm {
  const selected = new Set(form.selected);
  if (selected.has(key)) selected.delete(key);
  else selected.add(key);
  return { ...form, selected };
}

/** Changing a child's project carries the sub-tasks that still followed it. */
export function setProject(
  form: ImportForm,
  preview: JiraEpicPreview,
  key: string,
  project: string,
): ImportForm {
  const projects = { ...form.projects, [key]: project };
  const child = preview.children.find((c) => c.key === key);
  if (child) {
    const before = form.projects[key] ?? '';
    for (const st of child.subtasks ?? []) {
      if (!st.imported && (form.projects[st.key] ?? '') === before) projects[st.key] = project;
    }
  }
  return { ...form, projects };
}

export function setAllProjects(
  form: ImportForm,
  preview: JiraEpicPreview,
  project: string,
): ImportForm {
  const projects = { ...form.projects };
  for (const n of flatten(preview)) if (!n.imported) projects[n.key] = project;
  return { ...form, projects };
}

/** Sub-tasks whose project differs from their parent's: the dependency becomes order only. */
export function splitWarnings(preview: JiraEpicPreview, form: ImportForm): Map<string, string> {
  const inPlay = (n: JiraIssueNode) => form.selected.has(n.key) || !!n.imported;
  const warnings = new Map<string, string>();
  for (const child of preview.children) {
    const parentProject = form.projects[child.key];
    for (const st of child.subtasks ?? []) {
      const project = form.projects[st.key];
      if (parentProject && project && project !== parentProject && inPlay(child) && inPlay(st)) {
        warnings.set(st.key, child.key);
      }
    }
  }
  return warnings;
}

/**
 * Selected issues whose target project's card prefix equals a Jira project
 * key in this import. Such a card ID (PROJ-801) in a PR body or commit reads
 * as a Jira key, and GitHub for Jira may link the PR to the wrong issue.
 */
export function prefixClashes(
  preview: JiraEpicPreview,
  form: ImportForm,
  projects: ProjectConfig[],
): Set<string> {
  const jiraKeys = new Set(flatten(preview).map((n) => n.jira_project.toUpperCase()));
  jiraKeys.add(preview.epic.key.slice(0, preview.epic.key.lastIndexOf('-')).toUpperCase());
  const prefixOf = new Map(projects.map((p) => [p.name, (p.prefix ?? '').toUpperCase()]));
  const clashes = new Set<string>();
  for (const n of flatten(preview)) {
    const prefix = prefixOf.get(form.projects[n.key]);
    if (form.selected.has(n.key) && prefix && jiraKeys.has(prefix)) clashes.add(n.key);
  }
  return clashes;
}

export function canSubmit(preview: JiraEpicPreview, form: ImportForm): boolean {
  if (preview.playbook?.run_active) return false;
  if (form.selected.size === 0) return false;
  if (!preview.playbook && !form.title.trim()) return false;
  return flatten(preview).every((n) => !form.selected.has(n.key) || !!form.projects[n.key]);
}

export function buildRequest(
  preview: JiraEpicPreview,
  form: ImportForm,
  description: string,
  boardsRepo?: string,
): JiraImportRequest {
  return {
    selections: flatten(preview)
      .filter((n) => form.selected.has(n.key))
      .map((n) => ({ key: n.key, project: form.projects[n.key] })),
    vetted: form.vetted,
    include_epic_context: form.includeEpic,
    playbook: {
      title: form.title.trim(),
      description: description || preview.epic.summary,
      ...(boardsRepo ? { boards_repo: boardsRepo } : {}),
    },
  };
}
