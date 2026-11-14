import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { api } from '../../api/client';
import { useAttention } from '../../hooks/useAttention';
import { useJiraStatus } from '../../hooks/useJiraStatus';
import { usePlaybooks } from '../../hooks/usePlaybooks';
import { useTheme } from '../../hooks/useTheme';
import { jiraAuthProblem, jiraImportBlocked } from '../../lib/jiraStatus';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { JiraImportDialog } from '../JiraImport/JiraImportDialog';
import { SessionsBulkBar } from '../Sessions/SessionsBulkBar';
import { SessionsEmpty } from '../Sessions/SessionsEmpty';
import { useBulkActions } from '../Sessions/useBulkActions';
import { useRowSelection } from '../Sessions/useRowSelection';
import { PlaybookCreateRow } from './PlaybookCreateRow';
import { EMPTY_PLAYBOOK_FILTER, filterPlaybooks, isEmptyPlaybookFilter, newestFirst, type PlaybookFilter } from './playbookFilter';
import { PlaybooksTable } from './PlaybooksTable';
import { PlaybooksTabToolbar } from './PlaybooksTabToolbar';
import { usePlaybookCreateForm } from './usePlaybookCreateForm';
import { usePlaybookPin } from './usePlaybookPin';

const plural = (n: number) => `${n} playbook${n === 1 ? '' : 's'}`;

interface PlaybooksTabProps {
  /** Held by the page so it outlives a tab switch. */
  filter: PlaybookFilter;
  onFilterChange: (next: PlaybookFilter) => void;
}

/** The Sessions page's Playbooks tab: every playbook in one table, with filters, pins, bulk actions and the create row. */
export function PlaybooksTab({ filter, onFilterChange }: PlaybooksTabProps) {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const { playbooks, loaded, error, refresh } = usePlaybooks();
  const pin = usePlaybookPin();
  const { playbooks: waiting } = useAttention();
  const [confirming, setConfirming] = useState(false);
  const {
    creating, setCreating, newTitle, setNewTitle, newDescription, setNewDescription,
    newRepo, setNewRepo, submitting, boardsRepos, multiRepo, handleCreate, cancelCreate,
  } = usePlaybookCreateForm();
  // The toolbar reads the config flag; the probe runs only while the row is open.
  const { jiraEnabled } = useTheme();
  const jira = useJiraStatus(creating);
  const [importKey, setImportKey] = useState<string | null>(null);

  // `?new=1` opens the row during render; the effect then strips it, so it opens once.
  const wantsNew = params.get('new') === '1';
  if (wantsNew && !creating) setCreating(true);
  useEffect(() => {
    if (!wantsNew) return;
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete('new');
      return next;
    }, { replace: true });
  }, [wantsNew, setParams]);

  const rows = newestFirst(filterPlaybooks(playbooks, filter));
  const { picked, selected, toggle, toggleAll, tickOnly } = useRowSelection(rows.map((p) => p.id));
  const { busy, run, deleteTicked } = useBulkActions({ selected, tickOnly });

  const createRow = creating && (
    <PlaybookCreateRow
      title={newTitle}
      onTitleChange={setNewTitle}
      description={newDescription}
      onDescriptionChange={setNewDescription}
      onCreate={handleCreate}
      onCancel={cancelCreate}
      submitting={submitting}
      repos={boardsRepos}
      repo={newRepo || boardsRepos[0]?.name}
      onRepoChange={setNewRepo}
      jiraSiteURL={jira?.configured ? jira.site_url : undefined}
      jiraAuthProblem={jiraAuthProblem(jira) ?? undefined}
      jiraImportBlocked={jiraImportBlocked(jira)}
      onImportJira={setImportKey}
    />
  );

  return (
    <>
      <PlaybooksTabToolbar
        playbooks={playbooks}
        filter={filter}
        onChange={onFilterChange}
        onNew={() => setCreating(true)}
        creating={creating}
        jiraEnabled={jiraEnabled}
      />
      {error && <p className="ssn-error" role="alert">Could not load playbooks: {error}</p>}
      {selected.length > 0 && (
        <SessionsBulkBar
          label="Selected playbooks"
          count={selected.length}
          busy={busy}
          onPin={() => void run(() => pin(selected, true))}
          onUnpin={() => void run(() => pin(selected, false))}
          onDelete={() => setConfirming(true)}
        />
      )}
      {creating || (loaded && rows.length > 0) ? (
        <PlaybooksTable
          rows={rows}
          createRow={createRow}
          selected={picked}
          onToggle={toggle}
          onToggleAll={toggleAll}
          onPin={(p) => void pin([p.id], !p.pinned)}
          onOpen={(id) => navigate(`/playbooks/${id}`)}
          waiting={waiting}
        />
      ) : !loaded ? (
        !error && <p className="ssn-loading">Loading…</p>
      ) : (
        !error && (
          <SessionsEmpty
            emptyText="No playbooks yet."
            filtered={playbooks.length > 0 && !isEmptyPlaybookFilter(filter)}
            onClear={() => onFilterChange(EMPTY_PLAYBOOK_FILTER)}
          />
        )
      )}
      {importKey && (
        <JiraImportDialog
          epicKey={importKey}
          description={newDescription.trim()}
          boardsRepo={multiRepo ? newRepo || boardsRepos[0].name : undefined}
          onClose={() => setImportKey(null)}
        />
      )}
      <ConfirmModal
        open={confirming}
        title={`Delete ${plural(selected.length)}?`}
        message="This removes each playbook; its cards stay on their boards and git keeps its history. A playbook with an active run is not deleted until the run is stopped."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={() => {
          setConfirming(false);
          void deleteTicked((id) => api.deletePlaybook(id), refresh, plural);
        }}
        onCancel={() => setConfirming(false)}
      />
    </>
  );
}
