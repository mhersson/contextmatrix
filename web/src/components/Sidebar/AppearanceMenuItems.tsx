import { useTheme } from '../../hooks/useTheme';

type ThemeCtx = ReturnType<typeof useTheme>;
type Theme = ThemeCtx['theme'];
type Palette = ThemeCtx['palette'];

const THEMES: { id: Theme; label: string }[] = [
  { id: 'light', label: 'Light' },
  { id: 'dark', label: 'Dark' },
];

const PALETTES: { id: Palette; label: string }[] = [
  { id: 'everforest', label: 'Everforest' },
  { id: 'radix', label: 'Radix' },
  { id: 'catppuccin', label: 'Catppuccin' },
];

function RadioItem({ label, checked, onSelect }: { label: string; checked: boolean; onSelect: () => void }) {
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      onClick={onSelect}
      className="w-full flex items-center gap-2 text-left px-3 py-1.5 text-sm hover:opacity-80"
      style={{ color: checked ? 'var(--fg)' : 'var(--grey2)', fontWeight: checked ? 600 : 400 }}
    >
      <span aria-hidden="true" className="inline-block w-[1em]" style={{ color: 'var(--green)' }}>
        {checked ? '✓' : ''}
      </span>
      {label}
    </button>
  );
}

/**
 * APPEARANCE group for the sidebar-footer menus: theme (light/dark) and
 * palette as radio items. Shared by UserMenu (multi mode) and AppearanceMenu
 * (none mode) so both modes expose the identical controls.
 */
export function AppearanceMenuItems() {
  const { theme, palette, setTheme, setPalette } = useTheme();
  return (
    <>
      <div className="px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wide" style={{ color: 'var(--grey0)' }}>
        APPEARANCE
      </div>
      <div className="sb-eyebrow px-3 pt-1 pb-0.5">Theme</div>
      {THEMES.map((t) => (
        <RadioItem key={t.id} label={t.label} checked={theme === t.id} onSelect={() => setTheme(t.id)} />
      ))}
      <div className="sb-eyebrow px-3 pt-1.5 pb-0.5">Palette</div>
      {PALETTES.map((p) => (
        <RadioItem key={p.id} label={p.label} checked={palette === p.id} onSelect={() => setPalette(p.id)} />
      ))}
    </>
  );
}
