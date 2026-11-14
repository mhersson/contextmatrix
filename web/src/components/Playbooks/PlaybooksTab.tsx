import { useState } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../../api/client';
import { usePlaybooks } from '../../hooks/usePlaybooks';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { SessionsBulkBar } from '../Sessions/SessionsBulkBar';
import { SessionsEmpty } from '../Sessions/SessionsEmpty';
import { useBulkActions } from '../Sessions/useBulkActions';
import { useRowSelection } from '../Sessions/useRowSelection';
import { EMPTY_PLAYBOOK_FILTER, filterPlaybooks, isEmptyPlaybookFilter, newestFirst, type PlaybookFilter } from './playbookFilter';
import { PlaybooksTable } from './PlaybooksTable';
import { PlaybooksTabToolbar } from './PlaybooksTabToolbar';
import { usePlaybookPin } from './usePlaybookPin';

const plural = (n: number) => `${n} playbook${n === 1 ? '' : 's'}`;

interface PlaybooksTabProps {
  /** Held by the page so it outlives a tab switch. */
  filter: PlaybookFilter;
  onFilterChange: (next: PlaybookFilter) => void;
}

/** The Sessions page's Playbooks tab: every playbook in one table, with filters, pins and bulk actions. */
export function PlaybooksTab({ filter, onFilterChange }: PlaybooksTabProps) {
  const navigate = useNavigate();
  const { playbooks, loaded, error, refresh } = usePlaybooks();
  const pin = usePlaybookPin();
  const [confirming, setConfirming] = useState(false);

  const rows = newestFirst(filterPlaybooks(playbooks, filter));
  const { picked, selected, toggle, toggleAll, tickOnly } = useRowSelection(rows.map((p) => p.id));
  const { busy, run, deleteTicked } = useBulkActions({ selected, tickOnly });

  return (
    <>
      <PlaybooksTabToolbar playbooks={playbooks} filter={filter} onChange={onFilterChange} />
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
      {!loaded ? (
        !error && <p className="ssn-loading">Loading…</p>
      ) : rows.length > 0 ? (
        <PlaybooksTable
          rows={rows}
          selected={picked}
          onToggle={toggle}
          onToggleAll={toggleAll}
          onPin={(p) => void pin([p.id], !p.pinned)}
          onOpen={(id) => navigate(`/playbooks/${id}`)}
        />
      ) : (
        !error && (
          <SessionsEmpty
            emptyText="No playbooks yet."
            filtered={playbooks.length > 0 && !isEmptyPlaybookFilter(filter)}
            onClear={() => onFilterChange(EMPTY_PLAYBOOK_FILTER)}
          />
        )
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
