import { useState, useCallback, useContext, createContext } from 'react';
import type { ReactNode } from 'react';

interface MobileSidebarContextValue {
  isOpen: boolean;
  toggle: () => void;
  close: () => void;
}

const MobileSidebarContext = createContext<MobileSidebarContextValue | null>(null);

export function MobileSidebarProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);

  const toggle = useCallback(() => {
    setIsOpen((prev) => !prev);
  }, []);

  const close = useCallback(() => {
    setIsOpen(false);
  }, []);

  return (
    <MobileSidebarContext.Provider value={{ isOpen, toggle, close }}>
      {children}
    </MobileSidebarContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useMobileSidebar(): MobileSidebarContextValue {
  const ctx = useContext(MobileSidebarContext);
  if (!ctx) {
    throw new Error('useMobileSidebar must be used within a MobileSidebarProvider');
  }
  return ctx;
}
