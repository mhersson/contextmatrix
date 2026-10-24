import { useId, useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { useSessionPin } from '../../hooks/useSessionPin';
import { SESSIONS_TAB } from '../../lib/sessions';
import { safeGetString, safeSetString } from '../../utils/safeStorage';
import type { ChatSession, SessionKind } from '../../types';
import { sectionRows } from './sectionRows';
import { SessionRow } from './SessionRow';

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
  const headerId = useId();
  const listId = useId();
  const [collapsed, setCollapsed] = useState<boolean>(() => safeGetString(cfg.storageKey) === '1');

  const toggle = () => {
    setCollapsed((c) => {
      const next = !c;
      safeSetString(cfg.storageKey, next ? '1' : '0');
      return next;
    });
  };

  const handleOpen = (id: string) => {
    // ChatPage opens /chat/<id> as a pane on top of the persisted layout.
    navigate(`/chat/${id}`);
    mobileSidebar.close();
  };

  const handleNew = () => {
    onNew?.();
    mobileSidebar.close();
  };

  return (
    <section className="sb-chat-dock my-1.5 px-2 py-1" aria-labelledby={headerId}>
      <div className="flex items-center justify-between px-3 py-1">
        <button
          id={headerId}
          type="button"
          onClick={toggle}
          className="sb-eyebrow flex items-center gap-1.5"
          aria-expanded={!collapsed}
          aria-controls={listId}
        >
          <span aria-hidden="true" className="text-[8px]">{collapsed ? '▸' : '▼'}</span>
          {cfg.label}
        </button>
        {onNew && (
          <button type="button" onClick={handleNew} className="sb-eyebrow-btn" aria-label={cfg.newLabel} title={cfg.newLabel}>
            + new
          </button>
        )}
      </div>
      {!collapsed && (
        <div id={listId} className="mt-0.5 pb-0.5">
          {sessions.length === 0 ? (
            loaded && (
              <p className="px-3 py-2 text-xs italic" style={{ color: 'var(--grey1)' }}>
                {cfg.empty}
              </p>
            )
          ) : (
            <>
              <ul className="space-y-0.5">
                {sectionRows(sessions).map((s) => (
                  <SessionRow
                    key={s.id}
                    session={s}
                    onOpen={handleOpen}
                    onTogglePin={(row) => void setPinned([row.id], row.pinned !== true)}
                  />
                ))}
              </ul>
              <Link to={`/sessions?tab=${SESSIONS_TAB[kind]}`} onClick={mobileSidebar.close} className="sb-all">
                <span>
                  {cfg.all} <span aria-hidden="true">→</span>
                </span>
                <span className="sb-all__count">{sessions.length}</span>
              </Link>
            </>
          )}
        </div>
      )}
    </section>
  );
}
