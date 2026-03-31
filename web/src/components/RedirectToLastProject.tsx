import { Navigate } from 'react-router-dom';
import { useProjects } from '../hooks/useProjects';
import { useLastProject } from '../hooks/useLastProject';

export function RedirectToLastProject() {
  const { projects, loading } = useProjects();
  const [lastProject] = useLastProject();

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>Loading projects...</div>
      </div>
    );
  }

  if (projects.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>No projects yet. Create one using the sidebar.</div>
      </div>
    );
  }

  const target = lastProject && projects.some((p) => p.name === lastProject)
    ? lastProject
    : projects[0].name;

  return <Navigate to={`/projects/${target}`} replace />;
}
