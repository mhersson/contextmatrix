import { Link } from 'react-router';
import { SidebarMenuButton } from '../SidebarMenuButton/SidebarMenuButton';

interface ProjectSettingsCrumbProps {
  project: string;
  onOpenSidebar?: () => void;
}

/**
 * Crumb row for the project settings page - `Projects • <project> • Settings`
 * in the board band's crumb style, with the project segment linking back to
 * the board. Carries the mobile sidebar opener so Settings keeps navigation
 * now that there is no bar above the page.
 */
export function ProjectSettingsCrumb({ project, onOpenSidebar }: ProjectSettingsCrumbProps) {
  return (
    <div className="settings-crumb">
      {onOpenSidebar && <SidebarMenuButton onClick={onOpenSidebar} />}
      <div className="board-band__crumb">
        <span>Projects</span>
        <span className="dot" />
        <Link to={`/projects/${project}`} className="link">{project}</Link>
        <span className="dot" />
        <span className="accent">Settings</span>
      </div>
    </div>
  );
}
