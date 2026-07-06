import { useState, useCallback, lazy, Suspense } from 'react';
import { Routes, Route, useNavigate } from 'react-router-dom';
import { SSEProvider } from './hooks/useSSEBus';
import { ProjectsProvider } from './hooks/useProjects';
import { ProjectSummariesProvider } from './hooks/ProjectSummariesProvider';
import { ThemeProvider } from './hooks/useTheme';
import { AuthProvider } from './hooks/useAuth';
import { ToastContext, useToastState } from './hooks/useToast';
import { MobileSidebarProvider, useMobileSidebar } from './context/MobileSidebarContext';
import { ConsoleStateProvider } from './context/ConsoleStateContext';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Sidebar } from './components/Sidebar';
import { ToastContainer } from './components/Toast';
import { AuthGate } from './components/Auth';
import type { ProjectConfig } from './types';

// Lazy-load top-level routes so the initial bundle only contains the shell.
const ProjectShell = lazy(() =>
  import('./components/ProjectShell').then((m) => ({ default: m.ProjectShell }))
);
const ChatPage = lazy(() =>
  import('./pages/Chat/ChatPage').then((m) => ({ default: m.ChatPage }))
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
const AdminUsersPage = lazy(() =>
  import('./components/Admin').then((m) => ({ default: m.AdminUsersPage }))
);
const AdminCredentialsPage = lazy(() =>
  import('./components/Admin').then((m) => ({ default: m.AdminCredentialsPage }))
);
const AdminChatsPage = lazy(() =>
  import('./components/Admin').then((m) => ({ default: m.AdminChatsPage }))
);
const AdminModelSelectionPage = lazy(() =>
  import('./components/Admin').then((m) => ({ default: m.AdminModelSelectionPage }))
);
const AdminGuard = lazy(() =>
  import('./components/Admin').then((m) => ({ default: m.AdminGuard }))
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
          <ProjectSummariesProvider>
            <div className="h-screen flex flex-row" style={{ backgroundColor: 'var(--bg-dim)' }}>
              <Sidebar
                onNewProject={() => setNewProjectOpen(true)}
                onNewChat={() => navigate('/chat?new=1')}
                mobileOpen={mobileOpen}
                onMobileClose={onMobileClose}
              />

              <div className="flex-1 flex flex-col min-w-0">
                <ErrorBoundary>
                  <Suspense fallback={<AppShellSkeleton />}>
                    <Routes>
                      <Route index element={<AllProjectsDashboard onNewProject={() => setNewProjectOpen(true)} />} />
                      <Route path="projects/:project/*" element={<ProjectShell />} />
                      <Route path="all" element={<AllProjectsDashboard onNewProject={() => setNewProjectOpen(true)} />} />
                      <Route path="chat" element={<ChatPage />} />
                      <Route path="chat/:id" element={<ChatPage />} />
                      <Route path="admin/users" element={<AdminGuard><AdminUsersPage /></AdminGuard>} />
                      <Route path="admin/credentials" element={<AdminGuard><AdminCredentialsPage /></AdminGuard>} />
                      <Route path="admin/chats" element={<AdminGuard><AdminChatsPage /></AdminGuard>} />
                      <Route path="admin/model-selection" element={<AdminGuard><AdminModelSelectionPage /></AdminGuard>} />
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
          </ProjectSummariesProvider>
        </ProjectsProvider>
      </SSEProvider>
    </ToastContext.Provider>
  );
}

function App() {
  return (
    <AuthProvider>
      <ThemeProvider>
        <AuthGate>
          <MobileSidebarProvider>
            <ConsoleStateProvider>
              <AppInner />
            </ConsoleStateProvider>
          </MobileSidebarProvider>
        </AuthGate>
      </ThemeProvider>
    </AuthProvider>
  );
}

export default App;
