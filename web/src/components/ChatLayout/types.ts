export type Slot = 'TL' | 'BL' | 'TR' | 'BR';

export const SLOTS: Slot[] = ['TL', 'BL', 'TR', 'BR'];

export interface Pane {
  // `null` chatId = empty pane (created via split, awaiting "Pick a chat")
  chatId: string | null;
}

export type PaneSlots = Record<Slot, Pane | null>;

export interface PaneSizes {
  col: number;       // 0..100, left column width %
  leftRow: number;   // 0..100, left column's top-row height %
  rightRow: number;  // 0..100, right column's top-row height %
}

export interface AvailableChat {
  id: string;
  title: string;
  status?: string;
  model?: string;
}

export const EMPTY_PANES: PaneSlots = { TL: null, BL: null, TR: null, BR: null };
export const DEFAULT_SIZES: PaneSizes = { col: 50, leftRow: 50, rightRow: 50 };
