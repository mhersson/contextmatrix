import { useChatSessions } from '../../hooks/useChatSessions';
import { useTheme } from '../../hooks/useTheme';
import { sessionKind } from '../../lib/sessions';
import type { SessionKind } from '../../types';
import { SessionSection } from './SessionSection';

export function SessionSections({ onNewSession }: { onNewSession: (kind: SessionKind) => void }) {
  const { sessions, loaded } = useChatSessions();
  const { architectEnabled } = useTheme();
  const chats = sessions.filter((s) => sessionKind(s) === 'chat');
  const blueprints = sessions.filter((s) => sessionKind(s) === 'architect');

  return (
    <>
      <SessionSection kind="chat" sessions={chats} loaded={loaded} onNew={() => onNewSession('chat')} />
      {/* Without an architect the section stays only to reach blueprints an earlier configuration made. */}
      {(architectEnabled || blueprints.length > 0) && (
        <SessionSection
          kind="architect"
          sessions={blueprints}
          loaded={loaded}
          onNew={architectEnabled ? () => onNewSession('architect') : undefined}
        />
      )}
    </>
  );
}
