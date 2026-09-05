import { NewPlaybookButton, PlaceholderTrack, PlaybookGhostRow, type PlaybookGhostRowProps } from './PlaybookGhostRow';

interface PlaybooksEmptyHeroProps extends PlaybookGhostRowProps {
  creating: boolean;
  onStartCreate: () => void;
}

export function PlaybooksEmptyHero({ creating, onStartCreate, ...form }: PlaybooksEmptyHeroProps) {
  return (
    <div className="pbl-empty">
      <PlaceholderTrack nodes={5} gate={3} />
      <h1 className="pbl-empty-title">No playbooks yet</h1>
      <p className="pbl-empty-sub">
        A playbook is an ordered route of cards and manual steps, across projects.
        Progress follows the cards as agents finish them.
      </p>
      {creating ? (
        <div className="pbl-empty-ghost"><PlaybookGhostRow {...form} /></div>
      ) : (
        <NewPlaybookButton onClick={onStartCreate} className="pbl-empty-cta" />
      )}
    </div>
  );
}
