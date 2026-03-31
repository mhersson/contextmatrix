import { useMemo } from 'react';
import { useProjects } from '../../hooks/useProjects';
import { useAllBoards } from '../../hooks/useAllBoards';
import { SwimlaneRow } from './SwimlaneRow';

export function SwimlaneView() {
  const { projects } = useProjects();
  const projectNames = useMemo(() => projects.map((p) => p.name), [projects]);
  const { boards, loading, error } = useAllBoards(projectNames);

  const allStates = useMemo(() => {
    const seen = new Set<string>();
    const ordered: string[] = [];
    for (const name of projectNames) {
      const board = boards.get(name);
      if (!board) continue;
      for (const state of board.config.states) {
        if (!seen.has(state)) {
          seen.add(state);
          ordered.push(state);
        }
      }
    }
    return ordered;
  }, [projectNames, boards]);

  if (loading && boards.size === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>Loading all projects...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 rounded m-4" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
        {error}
      </div>
    );
  }

  if (projectNames.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>No projects yet.</div>
      </div>
    );
  }

  return (
    <div className="p-4 h-full overflow-auto">
      <h2 className="text-lg font-semibold mb-4" style={{ color: 'var(--fg)' }}>
        All Projects
      </h2>
      <div className="overflow-x-auto">
        <table className="border-separate" style={{ borderSpacing: '4px' }}>
          <thead>
            <tr>
              <th
                className="px-3 py-2 text-left text-sm font-medium sticky left-0 z-10"
                style={{ backgroundColor: 'var(--bg-dim)', color: 'var(--grey1)' }}
              >
                Project
              </th>
              {allStates.map((state) => (
                <th
                  key={state}
                  className="px-3 py-2 text-left text-sm font-medium min-w-[160px]"
                  style={{ color: 'var(--grey1)' }}
                >
                  {state.replace(/_/g, ' ')}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {projectNames.map((name) => {
              const board = boards.get(name);
              return (
                <SwimlaneRow
                  key={name}
                  project={name}
                  cards={board?.cards || []}
                  states={allStates}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
