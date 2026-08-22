import { Link } from 'react-router';
import { SidebarMenuButton } from '../SidebarMenuButton/SidebarMenuButton';

interface ProjectCrumbProps {
  project: string;
  /** Sub-page segment (e.g. "Settings"); when set, the project segment links back to the board. */
  current?: string;
  onOpenSidebar?: () => void;
}

/**
 * Crumb row in the board band's style for project routes that render no band:
 * the settings page (`Projects • <project> • Settings`) and the board's
 * loading / error states (`Projects • <project>`). Carries the mobile sidebar
 * opener so those states keep navigation now that nothing sits above them.
 */
export function ProjectCrumb({ project, current, onOpenSidebar }: ProjectCrumbProps) {
  return (
    <div className="project-crumb">
      {onOpenSidebar && <SidebarMenuButton onClick={onOpenSidebar} />}
      <div className="board-band__crumb">
        <span>Projects</span>
        <span className="dot" />
        {current ? (
          <>
            <Link to={`/projects/${project}`} className="link">{project}</Link>
            <span className="dot" />
            <span className="accent">{current}</span>
          </>
        ) : (
          <span className="accent">{project}</span>
        )}
      </div>
    </div>
  );
}
