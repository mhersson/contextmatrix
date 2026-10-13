import { useState, useEffect, useCallback, useMemo, createContext, useContext } from 'react';
import type { ProjectConfig } from '../types';
import { api, isAPIError } from '../api/client';
import { useSSEBus } from './useSSEBus';
import { useTheme } from './useTheme';
import { sortProjectsForSidebar } from '../lib/projectOrder';

interface ProjectsContextValue {
  projects: ProjectConfig[];
  loading: boolean;
  error: string | null;
  connected: boolean;
  refreshProjects: () => Promise<void>;
}

const ProjectsContext = createContext<ProjectsContextValue | null>(null);

export function ProjectsProvider({ children }: { children: React.ReactNode }) {
  const [rawProjects, setProjects] = useState<ProjectConfig[]>([]);
  const { boardsRepos } = useTheme();
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
    let cancelled = false;
    api.getProjects()
      .then((p) => {
        if (cancelled) return;
        setProjects(p);
        setError(null);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(isAPIError(err) ? err.error : 'Failed to load projects');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // The API returns projects in map order; every consumer sees sidebar order.
  const projects = useMemo(
    () => sortProjectsForSidebar(rawProjects, boardsRepos),
    [rawProjects, boardsRepos],
  );

  const { subscribe, connected } = useSSEBus();

  useEffect(() => {
    return subscribe('project.*', () => {
      loadProjects();
    });
  }, [subscribe, loadProjects]);

  const value = useMemo(
    () => ({ projects, loading, error, connected, refreshProjects: loadProjects }),
    [projects, loading, error, connected, loadProjects],
  );

  return (
    <ProjectsContext.Provider value={value}>
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
