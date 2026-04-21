import { useState, useCallback, lazy, Suspense } from 'react';
import { Routes, Route, useNavigate } from 'react-router-dom';
import { SSEProvider } from './hooks/useSSEBus';
import { ProjectsProvider } from './hooks/useProjects';
import { ThemeProvider } from './hooks/useTheme';
import { ToastContext, useToastState } from './hooks/useToast';
import { MobileSidebarProvider, useMobileSidebar } from './context/MobileSidebarContext';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Sidebar } from './components/Sidebar';
import { RedirectToLastProject } from './components/RedirectToLastProject';
import { ToastContainer } from './components/Toast';
import type { ProjectConfig } from './types';

// Lazy-load top-level routes so the initial bundle only contains the shell.
const ProjectShell = lazy(() =>
  import('./components/ProjectShell').then((m) => ({ default: m.ProjectShell }))
);
const AllProjectsDashboard = lazy(() =>
  import('./components/AllProjectsDashboard').then((m) => ({ default: m.AllProjectsDashboard }))
);
const NewProjectWizard = lazy(() =>
  import('./components/NewProjectWizard/NewProjectWizard').then((m) => ({ default: m.NewProjectWizard }))
);
const NotFound = lazy(() =>
  import('./components/NotFound').then((m) => ({ default: m.NotFound }))
);

/** Minimal placeholder shown while a lazy-loaded route chunk is being fetched. */
function AppShellSkeleton() {
  return (
    <div className="flex items-center justify-center h-full" style={{ color: 'var(--grey1)' }}>
      <div className="text-sm">Loading...</div>
    </div>
  );
}

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
                <Suspense fallback={<AppShellSkeleton />}>
                  <Routes>
                    <Route index element={<RedirectToLastProject />} />
                    <Route path="projects/:project/*" element={<ProjectShell />} />
                    <Route path="all" element={<AllProjectsDashboard />} />
                    <Route path="*" element={<NotFound />} />
                  </Routes>
                </Suspense>
              </ErrorBoundary>
            </div>

            {newProjectOpen && (
              <Suspense fallback={null}>
                <NewProjectWizard
                  onClose={() => setNewProjectOpen(false)}
                  onCreated={handleProjectCreated}
                />
              </Suspense>
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
