import { createContext, useContext } from 'react';
import type { MockupReply } from '../../types';

export interface MockupPane {
  chatId: string;
  /** The session takes sends and its stream is connected. */
  live: boolean;
  /** The open mockup prompt; null while nothing waits for the person. */
  promptId: string | null;
  send?: (content: string, reply: MockupReply) => Promise<void>;
}

export const MockupPaneContext = createContext<MockupPane | null>(null);

export function useMockupPane(): MockupPane | null {
  return useContext(MockupPaneContext);
}
