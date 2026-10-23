import { createContext } from 'react';
import type { ChatSession } from '../../types';
import type { HandoffPrefill } from './handoff';

export interface HandoffContextValue {
  sessions: readonly ChatSession[];
  openHandoff: (prefill: HandoffPrefill) => void;
}

export const HandoffContext = createContext<HandoffContextValue | null>(null);
