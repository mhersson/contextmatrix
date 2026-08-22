import { useCallback, useState } from 'react';
import { useMediaQuery } from './useMediaQuery';
import { safeReadBool, safeWriteBool } from '../utils/safeStorage';

const STORAGE_KEY = 'contextmatrix-board-header-collapsed';

/**
 * Collapsed state for the board header chrome. A stored choice always wins;
 * with nothing stored, short viewports start collapsed so small laptops get
 * the vertical space without ever finding the toggle.
 */
export function useBoardHeaderCollapsed(): [boolean, () => void] {
  const shortViewport = useMediaQuery('(max-height: 800px)');
  const [choice, setChoice] = useState<boolean | undefined>(() => safeReadBool(STORAGE_KEY));
  const collapsed = choice ?? shortViewport;

  const toggle = useCallback(() => {
    const next = !collapsed;
    setChoice(next);
    safeWriteBool(STORAGE_KEY, next);
  }, [collapsed]);

  return [collapsed, toggle];
}
