import { idColor } from '../../utils/colorHash';

export function PaneAccentStripe({ chatId }: { chatId: string | null }) {
  const color = chatId ? idColor(chatId) : 'var(--bg4)';
  return (
    <span
      className="chat-pane-stripe"
      style={{ backgroundColor: color }}
      aria-hidden="true"
    />
  );
}
