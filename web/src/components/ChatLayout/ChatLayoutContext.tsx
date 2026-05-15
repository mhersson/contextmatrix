import { createContext, useContext } from 'react';
import type { UseChatLayoutResult } from '../../hooks/useChatLayout';

const ChatLayoutCtx = createContext<UseChatLayoutResult | null>(null);

export interface ChatLayoutProviderProps {
  value: UseChatLayoutResult;
  children: React.ReactNode;
}

export function ChatLayoutProvider({ value, children }: ChatLayoutProviderProps) {
  return <ChatLayoutCtx.Provider value={value}>{children}</ChatLayoutCtx.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useChatLayoutContext(): UseChatLayoutResult {
  const ctx = useContext(ChatLayoutCtx);
  if (!ctx) {
    throw new Error('useChatLayoutContext: must be used inside <ChatLayoutProvider>');
  }
  return ctx;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useChatLayoutContextOrNull(): UseChatLayoutResult | null {
  return useContext(ChatLayoutCtx);
}
