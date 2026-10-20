import { createContext, useContext } from 'react';

export interface MockupPane {
  chatId: string;
}

export const MockupPaneContext = createContext<MockupPane | null>(null);

export function useMockupPane(): MockupPane | null {
  return useContext(MockupPaneContext);
}
