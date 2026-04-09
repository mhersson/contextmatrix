import { useState, useEffect, useCallback } from 'react';
import { Routes, Route, useNavigate } from 'react-router-dom';
import { ProjectsProvider } from './hooks/useProjects';
import { ThemeProvider } from './hooks/useTheme';
import { ToastContext, useToastState } from './hooks/useToast';
import { MobileSidebarProvider, useMobileSidebar } from './context/MobileSidebarContext';
import { api } from './api/client';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Sidebar } from './components/Sidebar';
import { ProjectShell } from './components/ProjectShell';
import { RedirectToLastProject } from './components/RedirectToLastProject';
import { AllProjectsDashboard } from './components/AllProjectsDashboard';
import { NewProjectWizard } from './components/NewProjectWizard/NewProjectWizard';
import { JiraImportWizard } from './components/JiraImportWizard';
import { NotFound } from './components/NotFound';
import { ToastContainer } from './components/Toast';
import type { ProjectConfig, JiraImportResult } from './types';

function AppInner() {
  const toastState = useToastState();
  const navigate = useNavigate();
  const [newProjectOpen, setNewProjectOpen] = useState(false);
  const [jiraImportOpen, setJiraImportOpen] = useState(false);
  const [jiraConfigured, setJiraConfigured] = useState(false);
  const { isOpen: mobileOpen, close: onMobileClose } = useMobileSidebar();

  useEffect(() => {
    api.getJiraStatus().then((status) => setJiraConfigured(status.configured)).catch(() => {});
  }, []);

  const handleProjectCreated = useCallback(
    (config: ProjectConfig) => {
      setNewProjectOpen(false);
      toastState.showToast(`Project "${config.name}" created`, 'success');
      navigate(`/projects/${config.name}`);
    },
    [navigate, toastState]
  );

  const handleJiraImported = useCallback(
    (result: JiraImportResult) => {
      setJiraImportOpen(false);
      toastState.showToast(
        `Imported ${result.cards_imported} issues from Jira into "${result.project.name}"`,
        'success'
      );
      navigate(`/projects/${result.project.name}`);
    },
    [navigate, toastState]
  );

  return (
    <ToastContext.Provider value={toastState}>
      <ProjectsProvider>
        <div className="h-screen flex flex-row" style={{ backgroundColor: 'var(--bg-dim)' }}>
          <Sidebar
            onNewProject={() => setNewProjectOpen(true)}
            onJiraImport={() => setJiraImportOpen(true)}
            jiraConfigured={jiraConfigured}
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

          {jiraImportOpen && (
            <JiraImportWizard
              onClose={() => setJiraImportOpen(false)}
              onImported={handleJiraImported}
            />
          )}

          <ToastContainer />
        </div>
      </ProjectsProvider>
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
