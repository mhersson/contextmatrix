import { personaName } from '../Blueprint/stages';
import type { RoundChatMessage } from './parseRound';

/** A round's chat thread, oldest first: the person's lines as "You", the architect's under its persona. */
export function RoundThread({ chat }: { chat: readonly RoundChatMessage[] }) {
  return chat.map((m, i) => (
    <div key={i} className={m.from === 'user' ? 'me' : 'ai'}>
      <b>{m.from === 'user' ? 'You' : personaName(m.from)}:</b> {m.text}
    </div>
  ));
}
