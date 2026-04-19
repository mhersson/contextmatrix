import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import type { ProjectConfig } from '../types';
import { api, isAPIError } from '../api/client';
import { useSSEBus } from './useSSEBus';

interface ProjectsContextValue {
  projects: ProjectConfig[];
  loading: boolean;
  error: string | null;
  connected: boolean;
  refreshProjects: () => void;
}

const ProjectsContext = createContext<ProjectsContextValue | null>(null);

export function ProjectsProvider({ children }: { children: React.ReactNode }) {
  const [projects, setProjects] = useState<ProjectConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadProjects = useCallback(async () => {
    try {
      const p = await api.getProjects();
      setProjects(p);
      setError(null);
    } catch (err) {
      setError(isAPIError(err) ? err.error : 'Failed to load projects');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadProjects();
  }, [loadProjects]);

  const { subscribe, connected } = useSSEBus();

  useEffect(() => {
    return subscribe((event) => {
      if (event.type.startsWith('project.')) {
        loadProjects();
      }
    });
  }, [subscribe, loadProjects]);

  return (
    <ProjectsContext.Provider value={{ projects, loading, error, connected, refreshProjects: loadProjects }}>
      {children}
    </ProjectsContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useProjects() {
  const context = useContext(ProjectsContext);
  if (!context) {
    throw new Error('useProjects must be used within a ProjectsProvider');
  }
  return context;
}
