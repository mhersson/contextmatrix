import { useState, useCallback } from 'react';
import { Routes, Route, useNavigate } from 'react-router-dom';
import { SSEProvider } from './hooks/useSSEBus';
import { ProjectsProvider } from './hooks/useProjects';
import { ThemeProvider } from './hooks/useTheme';
import { ToastContext, useToastState } from './hooks/useToast';
import { MobileSidebarProvider, useMobileSidebar } from './context/MobileSidebarContext';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Sidebar } from './components/Sidebar';
import { ProjectShell } from './components/ProjectShell';
import { RedirectToLastProject } from './components/RedirectToLastProject';
import { AllProjectsDashboard } from './components/AllProjectsDashboard';
import { NewProjectWizard } from './components/NewProjectWizard/NewProjectWizard';
import { NotFound } from './components/NotFound';
import { ToastContainer } from './components/Toast';
import type { ProjectConfig } from './types';

function AppInner() {
  const toastState = useToastState();
  const navigate = useNavigate();
  const [newProjectOpen, setNewProjectOpen] = useState(false);
  const { isOpen: mobileOpen, close: onMobileClose } = useMobileSidebar();

  const handleProjectCreated = useCallback(
    (config: ProjectConfig) => {
      setNewProjectOpen(false);
      toastState.showToast(`Project "${config.name}" created`, 'success');
      navigate(`/projects/${config.name}`);
    },
    [navigate, toastState]
  );

  return (
    <ToastContext.Provider value={toastState}>
      <SSEProvider>
        <ProjectsProvider>
          <div className="h-screen flex flex-row" style={{ backgroundColor: 'var(--bg-dim)' }}>
            <Sidebar
              onNewProject={() => setNewProjectOpen(true)}
              mobileOpen={mobileOpen}
              onMobileClose={onMobileClose}
            />

            <div className="flex-1 flex flex-col min-w-0">
              <ErrorBoundary>
                <Routes>
                  <Route index element={<RedirectToLastProject />} />
                  <Route path="projects/:project/*" element={<ProjectShell />} />
                  <Route path="all" element={<AllProjectsDashboard />} />
                  <Route path="*" element={<NotFound />} />
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
      </SSEProvider>
    </ToastContext.Provider>
  );
}

function App() {
  return (
    <ThemeProvider>
      <MobileSidebarProvider>
        <AppInner />
      </MobileSidebarProvider>
    </ThemeProvider>
  );
}

export default App;
