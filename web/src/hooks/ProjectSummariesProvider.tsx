import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { useProjects } from './useProjects';
import {
  useProjectSummaries,
  type UseProjectSummariesResult,
} from './useProjectSummaries';

const ProjectSummariesContext = createContext<UseProjectSummariesResult | null>(
  null,
);

export function ProjectSummariesProvider({
  children,
}: {
  children: ReactNode;
}) {
  const { projects } = useProjects();
  const projectNames = useMemo(() => projects.map((p) => p.name), [projects]);
  const value = useProjectSummaries(projectNames);
  return (
    <ProjectSummariesContext.Provider value={value}>
      {children}
    </ProjectSummariesContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useProjectSummariesContext(): UseProjectSummariesResult {
  const ctx = useContext(ProjectSummariesContext);
  if (!ctx)
    throw new Error(
      'useProjectSummariesContext must be used within ProjectSummariesProvider',
    );
  return ctx;
}
