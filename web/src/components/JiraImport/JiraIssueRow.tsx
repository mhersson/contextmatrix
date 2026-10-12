import type { JiraIssueNode, ProjectConfig } from '../../types';
import { isSelectable } from './jiraImportModel';

interface JiraIssueRowProps {
  node: JiraIssueNode;
  checked: boolean;
  project: string;
  projects: ProjectConfig[];
  splitFrom?: string;
  prefixClash: boolean;
  onToggle: () => void;
  onProject: (project: string) => void;
}

export function JiraIssueRow({
  node,
  checked,
  project,
  projects,
  splitFrom,
  prefixClash,
  onToggle,
  onProject,
}: JiraIssueRowProps) {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-center gap-2 text-sm min-w-0">
        <input
          type="checkbox"
          aria-label={`Import ${node.key}`}
          checked={checked}
          disabled={!isSelectable(node)}
          onChange={onToggle}
          className="accent-[var(--green)]"
        />
        <a
          href={node.url}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-xs shrink-0"
          style={{ color: 'var(--aqua)' }}
        >
          {node.key}
        </a>
        <span className="flex-1 min-w-0 truncate" style={{ color: 'var(--fg)' }}>
          {node.summary}
        </span>
        <span
          className="text-xs shrink-0"
          style={{ color: node.status_category === 'done' ? 'var(--green)' : 'var(--grey1)' }}
        >
          {node.issue_type} · {node.status}
        </span>
        <select
          aria-label={`Project for ${node.key}`}
          value={project}
          disabled={!!node.imported}
          onChange={(e) => onProject(e.target.value)}
          className="bf-input text-xs w-36 shrink-0"
        >
          <option value="">Choose project</option>
          {projects.map((p) => (
            <option key={p.name} value={p.name}>
              {p.display_name || p.name}
            </option>
          ))}
        </select>
      </div>
      {node.imported && (
        <p className="text-xs ml-6" style={{ color: 'var(--grey1)' }}>
          Imported as {node.imported.card}
          {node.imported.in_playbook ? '' : ', not in this playbook'}
        </p>
      )}
      {splitFrom && (
        <p
          role="note"
          aria-label={`${node.key} split from ${splitFrom}`}
          className="text-xs ml-6"
          style={{ color: 'var(--yellow)' }}
        >
          Not in {splitFrom}&apos;s project: it runs before {splitFrom} in the playbook, with no
          dependency
        </p>
      )}
      {prefixClash && (
        <p
          role="note"
          aria-label={`${node.key} prefix clash`}
          className="text-xs ml-6"
          style={{ color: 'var(--yellow)' }}
        >
          PR links may attach to the wrong Jira issue: {project}&apos;s card IDs look like this Jira
          project&apos;s keys
        </p>
      )}
    </div>
  );
}
