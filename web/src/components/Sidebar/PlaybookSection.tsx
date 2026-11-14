import { useNavigate } from 'react-router';
import { useAttention } from '../../hooks/useAttention';
import { usePlaybooks } from '../../hooks/usePlaybooks';
import { sessionsTabHref } from '../../lib/sessions';
import { PlaybookNavRow } from './PlaybookNavRow';
import { playbookRows } from './playbookRows';
import { WorkSection } from './WorkSection';

export function PlaybookSection() {
  const { playbooks, loaded } = usePlaybooks();
  const navigate = useNavigate();
  const { playbooks: waiting } = useAttention();

  return (
    <WorkSection
      label="Playbooks"
      storageKey="sidebar.playbook_section_collapsed"
      emptyText="No playbooks yet."
      allLabel="All playbooks"
      allHref={sessionsTabHref('playbooks')}
      total={playbooks.length}
      onNew={() => navigate(`${sessionsTabHref('playbooks')}&new=1`)}
      newLabel="New playbook"
      loaded={loaded}
    >
      {playbookRows(playbooks).map((p) => (
        <PlaybookNavRow key={p.id} playbook={p} waiting={waiting.has(p.id)} />
      ))}
    </WorkSection>
  );
}
