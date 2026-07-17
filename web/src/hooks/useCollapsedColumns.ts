import { useProjectScopedSet } from './useProjectScopedSet';

const STORAGE_KEY = 'contextmatrix-collapsed-columns';

export function useCollapsedColumns(project: string, validStates: string[]): [Set<string>, (state: string) => void] {
  const { values, toggle } = useProjectScopedSet(STORAGE_KEY, project, validStates);

  return [values, toggle];
}
