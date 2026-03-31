import { useState, useCallback } from 'react';
import { Routes, Route, useNavigate } from 'react-router-dom';
import { ProjectsProvider } from './hooks/useProjects';
import { ThemeProvider } from './hooks/useTheme';
import { ToastContext, useToastState } from './hooks/useToast';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Sidebar } from './components/Sidebar';
import { ProjectShell } from './components/ProjectShell';
import { RedirectToLastProject } from './components/RedirectToLastProject';
import { SwimlaneView } from './components/Swimlane';
import { AllProjectsDashboard } from './components/AllProjectsDashboard';
import { NewProjectWizard } from './components/NewProjectWizard/NewProjectWizard';
import { ToastContainer } from './components/Toast';
import type { ProjectConfig } from './types';

function App() {
  const toastState = useToastState();
  const navigate = useNavigate();
  const [newProjectOpen, setNewProjectOpen] = useState(false);

  const handleProjectCreated = useCallback(
    (config: ProjectConfig) => {
      setNewProjectOpen(false);
      toastState.showToast(`Project "${config.name}" created`, 'success');
      navigate(`/projects/${config.name}`);
    },
    [navigate, toastState]
  );

  return (
    <ThemeProvider>
    <ToastContext.Provider value={toastState}>
      <ProjectsProvider>
        <div className="min-h-screen flex flex-row" style={{ backgroundColor: 'var(--bg-dim)' }}>
          <Sidebar onNewProject={() => setNewProjectOpen(true)} />

          <div className="flex-1 flex flex-col min-w-0">
            <ErrorBoundary>
              <Routes>
                <Route index element={<RedirectToLastProject />} />
                <Route path="projects/:project/*" element={<ProjectShell />} />
                <Route path="all" element={<SwimlaneView />} />
                <Route path="all/dashboard" element={<AllProjectsDashboard />} />
              </Routes>
            </ErrorBoundary>
          </div>

          {newProjectOpen && (
            <NewProjectWizard
              onClose={() => setNewProjectOpen(false)}
              onCreated={handleProjectCreated}
            />
          )}

          <ToastContainer />
        </div>
      </ProjectsProvider>
    </ToastContext.Provider>
    </ThemeProvider>
  );
}

export default App;
