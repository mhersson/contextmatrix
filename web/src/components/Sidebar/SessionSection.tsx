import { useNavigate } from 'react-router';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { useSessionPin } from '../../hooks/useSessionPin';
import { SESSIONS_TAB } from '../../lib/sessions';
import type { ChatSession, SessionKind } from '../../types';
import { sectionRows } from './sectionRows';
import { SessionRow } from './SessionRow';
import { WorkSection } from './WorkSection';

const SECTIONS = {
  chat: {
    label: 'Chats',
    storageKey: 'sidebar.chat_section_collapsed',
    empty: 'No chats yet.',
    all: 'All chats',
    newLabel: 'New chat',
  },
  architect: {
    label: 'Blueprints',
    storageKey: 'sidebar.blueprint_section_collapsed',
    empty: 'No blueprints yet.',
    all: 'All blueprints',
    newLabel: 'New blueprint',
  },
} as const;

interface SessionSectionProps {
  kind: SessionKind;
  sessions: ChatSession[];
  /** Omitted: the section has no `+ new`. */
  onNew?: () => void;
  /** False until the list first loads, so an empty section makes no claim yet. */
  loaded?: boolean;
}

export function SessionSection({ kind, sessions, onNew, loaded = true }: SessionSectionProps) {
  const cfg = SECTIONS[kind];
  const navigate = useNavigate();
  const mobileSidebar = useMobileSidebar();
  const setPinned = useSessionPin();

  const handleOpen = (id: string) => {
    // ChatPage opens /chat/<id> on top of the persisted layout.
    navigate(`/chat/${id}`);
    mobileSidebar.close();
  };

  return (
    <WorkSection
      label={cfg.label}
      storageKey={cfg.storageKey}
      emptyText={cfg.empty}
      allLabel={cfg.all}
      allHref={`/sessions?tab=${SESSIONS_TAB[kind]}`}
      total={sessions.length}
      onNew={onNew}
      newLabel={cfg.newLabel}
      loaded={loaded}
    >
      {sectionRows(sessions).map((s) => (
        <SessionRow
          key={s.id}
          session={s}
          onOpen={handleOpen}
          onTogglePin={(row) => void setPinned([row.id], row.pinned !== true)}
        />
      ))}
    </WorkSection>
  );
}
