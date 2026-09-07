/**
 * Palette registry. The id is the value stored in localStorage, sent by the
 * server as `theme`, and set as `data-palette` on <html> (everforest is the
 * unnamed default and sets no attribute). Each id needs a dark and a light
 * block in index.css, and the server-side allow list in
 * internal/config/config.go must carry the same ids.
 */
export const PALETTES = [
  { id: 'everforest', label: 'Everforest' },
  { id: 'catppuccin', label: 'Catppuccin' },
  { id: 'github', label: 'GitHub' },
  { id: 'ayu', label: 'Ayu' },
] as const;

export type Palette = (typeof PALETTES)[number]['id'];

export function isPalette(value: string): value is Palette {
  return PALETTES.some((p) => p.id === value);
}
