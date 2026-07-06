import { useState, useCallback, useMemo, useContext, createContext } from 'react';
import type { ReactNode } from 'react';

interface ConsoleStateContextValue {
  isOpen: boolean;
  toggle: () => void;
  close: () => void;
  setOpen: (v: boolean) => void;
}

const ConsoleStateContext = createContext<ConsoleStateContextValue | null>(null);

export function ConsoleStateProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);

  const toggle = useCallback(() => {
    setIsOpen((prev) => !prev);
  }, []);

  const close = useCallback(() => {
    setIsOpen(false);
  }, []);

  const setOpen = useCallback((v: boolean) => {
    setIsOpen(v);
  }, []);

  const value = useMemo(
    () => ({ isOpen, toggle, close, setOpen }),
    [isOpen, toggle, close, setOpen],
  );

  return (
    <ConsoleStateContext.Provider value={value}>
      {children}
    </ConsoleStateContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useConsoleState(): ConsoleStateContextValue {
  const ctx = useContext(ConsoleStateContext);
  if (!ctx) {
    throw new Error('useConsoleState must be used within a ConsoleStateProvider');
  }
  return ctx;
}
