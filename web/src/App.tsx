import { useState, useEffect } from 'react';
import { api } from './api/client';
import { useBoard } from './hooks/useBoard';
import { Board } from './components/Board';
import type { ProjectConfig } from './types';

function App() {
  const [projects, setProjects] = useState<ProjectConfig[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [projectsLoading, setProjectsLoading] = useState(true);
  const [projectsError, setProjectsError] = useState<string | null>(null);

  useEffect(() => {
    api
      .getProjects()
      .then((p) => {
        setProjects(p);
        if (p.length > 0 && !selectedProject) {
          setSelectedProject(p[0].name);
        }
      })
      .catch((err) => {
        setProjectsError(err?.error || 'Failed to load projects');
      })
      .finally(() => {
        setProjectsLoading(false);
      });
  }, [selectedProject]);

  const { config, cards, loading, error, connected } = useBoard(selectedProject);

  return (
    <div className="min-h-screen flex flex-col" style={{ backgroundColor: 'var(--bg-dim)' }}>
      <header
        className="flex items-center justify-between px-6 py-4 border-b"
        style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
      >
        <h1
          className="text-xl font-semibold"
          style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}
        >
          ContextMatrix
        </h1>

        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span
              className={`w-2 h-2 rounded-full ${connected ? 'animate-pulse' : ''}`}
              style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
            />
            <span className="text-sm" style={{ color: 'var(--grey1)' }}>
              {connected ? 'Connected' : 'Disconnected'}
            </span>
          </div>

          <select
            value={selectedProject}
            onChange={(e) => setSelectedProject(e.target.value)}
            disabled={projectsLoading || projects.length === 0}
            className="px-3 py-1.5 rounded text-sm border"
            style={{
              backgroundColor: 'var(--bg1)',
              borderColor: 'var(--bg3)',
              color: 'var(--fg)',
            }}
          >
            {projectsLoading && <option>Loading...</option>}
            {!projectsLoading && projects.length === 0 && (
              <option>No projects</option>
            )}
            {projects.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
      </header>

      <main className="flex-1 overflow-hidden">
        {projectsError && (
          <div
            className="p-4 rounded m-4"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
          >
            {projectsError}
          </div>
        )}

        {selectedProject && config ? (
          <Board
            cards={cards}
            config={config}
            loading={loading}
            error={error}
          />
        ) : (
          <div className="flex items-center justify-center h-full">
            <div style={{ color: 'var(--grey1)' }}>
              {projectsLoading ? 'Loading projects...' : 'Select a project to view the board'}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

export default App;
