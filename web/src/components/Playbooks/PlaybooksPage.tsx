import { useCallback, useEffect, useState } from 'react';
import { api } from '../../api/client';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useJiraStatus } from '../../hooks/useJiraStatus';
import { jiraAuthProblem, jiraImportBlocked } from '../../lib/jiraStatus';
import { JiraImportDialog } from '../JiraImport/JiraImportDialog';
import { useToast } from '../../hooks/useToast';
import type { PlaybookSummary } from '../../types';
import { isFullyComplete } from './playbookUtils';
import { PlaybookReceipt, PlaybookRow } from './PlaybookRow';
import { PlaybooksBar } from './PlaybooksBar';
import { NewPlaybookButton, PlaybookGhostRow } from './PlaybookGhostRow';
import { PlaybooksEmptyHero } from './PlaybooksEmptyHero';
import { usePlaybookCreateForm } from './usePlaybookCreateForm';

export function PlaybooksPage() {
  const [playbooks, setPlaybooks] = useState<PlaybookSummary[] | null>(null);
  // Ephemeral by design - no localStorage; folding state resets on reload.
  // Open by default so a board where everything is done never looks empty.
  const [completedOpen, setCompletedOpen] = useState(true);
  const { subscribe, reconnectEpoch } = useSSEBus();
  const {
    creating, setCreating, newTitle, setNewTitle, newDescription, setNewDescription,
    newRepo, setNewRepo, submitting, boardsRepos, multiRepo, handleCreate, cancelCreate,
  } = usePlaybookCreateForm();
  const jira = useJiraStatus(creating);
  const [importKey, setImportKey] = useState<string | null>(null);
  const { showToast } = useToast();

  const fetchAll = useCallback(() => {
    api.listPlaybooks().then(setPlaybooks).catch(() => showToast('Failed to load playbooks', 'error'));
  }, [showToast]);

  useEffect(() => { fetchAll(); }, [fetchAll, reconnectEpoch]);
  useEffect(() => subscribe('playbook.*', fetchAll), [subscribe, fetchAll]);
  // Progress and segments derive from live card states, so card events
  // must refresh the list too, not just playbook mutations.
  useEffect(() => subscribe('card.*', fetchAll), [subscribe, fetchAll]);

  const active = (playbooks ?? []).filter((p) => !isFullyComplete(p));
  const completed = (playbooks ?? []).filter(isFullyComplete);

  const ghostProps = {
    title: newTitle,
    onTitleChange: setNewTitle,
    description: newDescription,
    onDescriptionChange: setNewDescription,
    onCreate: handleCreate,
    onCancel: cancelCreate,
    submitting,
    repos: boardsRepos,
    repo: newRepo || boardsRepos[0]?.name,
    onRepoChange: setNewRepo,
    jiraSiteURL: jira?.configured ? jira.site_url : undefined,
    jiraAuthProblem: jiraAuthProblem(jira) ?? undefined,
    jiraImportBlocked: jiraImportBlocked(jira),
    onImportJira: setImportKey,
  };

  const isEmpty = playbooks !== null && active.length === 0 && completed.length === 0;

  // The dialog sits outside both layouts: the first import on an empty page
  // creates the playbook and flips the layout mid-run, which must not remount it.
  return (
    <>
      {isEmpty ? (
        <div className="h-full flex flex-col overflow-y-auto">
          <PlaybooksBar />
          <PlaybooksEmptyHero creating={creating} onStartCreate={() => setCreating(true)} {...ghostProps} />
        </div>
      ) : (
        <div className="h-full overflow-y-auto">
          <PlaybooksBar />
          <div className="pbl-page">
            <div className="pbl-header">
              <div>
                <h1 className="pbl-title">Playbooks</h1>
                {playbooks === null ? (
                  <p className="pbl-summary">Loading...</p>
                ) : (
                  <p className="pbl-summary"><b>{active.length}</b> in progress, <b>{completed.length}</b> completed</p>
                )}
              </div>
              <NewPlaybookButton onClick={() => setCreating(true)} disabled={creating} />
            </div>

            <div className="pbl-list">
              {creating && <PlaybookGhostRow {...ghostProps} />}
              {active.map((p) => <PlaybookRow key={p.id} playbook={p} />)}
            </div>

            {completed.length > 0 && (
              <>
                <button
                  type="button"
                  onClick={() => setCompletedOpen((v) => !v)}
                  aria-expanded={completedOpen}
                  className={`pbl-section${completedOpen ? ' pbl-section-open' : ''}`}
                >
                  <span className="pbl-section-chev" aria-hidden="true">▶</span>
                  <span>Completed</span>
                  <span className="pbl-section-count">{completed.length}</span>
                  <span className="pbl-section-line" aria-hidden="true" />
                </button>
                {completedOpen && (
                  <div className="pbl-receipts">
                    {completed.map((p) => <PlaybookReceipt key={p.id} playbook={p} />)}
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}
      {importKey && (
        <JiraImportDialog
          epicKey={importKey}
          description={newDescription.trim()}
          boardsRepo={multiRepo ? newRepo || boardsRepos[0].name : undefined}
          onClose={() => setImportKey(null)}
        />
      )}
    </>
  );
}
