import { usePlaybooks } from '../../hooks/usePlaybooks';
import { PlaybookNavRow } from './PlaybookNavRow';
import { playbookRows } from './playbookRows';
import { WorkSection } from './WorkSection';

export function PlaybookSection() {
  const { playbooks, loaded } = usePlaybooks();

  return (
    <WorkSection
      label="Playbooks"
      storageKey="sidebar.playbook_section_collapsed"
      emptyText="No playbooks yet."
      allLabel="All playbooks"
      allHref="/playbooks"
      total={playbooks.length}
      loaded={loaded}
    >
      {playbookRows(playbooks).map((p) => (
        <PlaybookNavRow key={p.id} playbook={p} />
      ))}
    </WorkSection>
  );
}
