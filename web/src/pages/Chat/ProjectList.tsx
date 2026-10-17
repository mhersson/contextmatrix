import { useId } from 'react';
import { repoLabel } from '../../lib/repoLabel';

export interface ProjectOption {
  name: string;
  display_name?: string;
  repo?: string;
}

interface ProjectListProps {
  projects: ProjectOption[];
  /** Multi-select picks any number; single-select starts with "Across projects" (name ''). */
  multi: boolean;
  selected: string[];
  onChange: (selected: string[]) => void;
}

const ACROSS: ProjectOption = { name: '', display_name: 'Across projects' };

export function ProjectList({ projects, multi, selected, onChange }: ProjectListProps) {
  const group = useId();
  const rows = multi ? projects : [ACROSS, ...projects];
  return (
    <div className="nd-projects" role="group" aria-label="Projects">
      {rows.map((p) => {
        const label = p.display_name ?? p.name;
        const checked = multi ? selected.includes(p.name) : (selected[0] ?? '') === p.name;
        const detail = p.name === '' ? 'No checkout' : repoLabel(p.repo);
        const pick = () =>
          onChange(multi ? (checked ? selected.filter((n) => n !== p.name) : [...selected, p.name]) : [p.name]);
        return (
          <label key={p.name || '(across)'} className={`nd-project${checked ? ' nd-project--on' : ''}`}>
            <input
              type={multi ? 'checkbox' : 'radio'}
              name={multi ? undefined : group}
              checked={checked}
              aria-label={label}
              onChange={pick}
            />
            <span className="nd-project-name">{label}</span>
            {detail && <span className="nd-project-repo">{detail}</span>}
          </label>
        );
      })}
    </div>
  );
}
