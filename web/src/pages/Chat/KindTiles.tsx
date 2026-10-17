import { useId, type KeyboardEvent, type ReactNode } from 'react';
import { BlueprintIcon, ChatIcon } from '../../components/SessionIcons';

interface KindTilesProps {
  blueprint: boolean;
  onChange: (blueprint: boolean) => void;
}

const ARROWS = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'];

export function KindTiles({ blueprint, onChange }: KindTilesProps) {
  // Two radios: any arrow key selects and focuses the other one.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!ARROWS.includes(e.key)) return;
    e.preventDefault();
    e.currentTarget.querySelector<HTMLElement>('[aria-checked="false"]')?.focus();
    onChange(!blueprint);
  };
  return (
    <div className="nd-kinds" role="radiogroup" aria-label="Session kind" onKeyDown={onKeyDown}>
      <Tile
        selected={!blueprint}
        onSelect={() => onChange(false)}
        icon={<ChatIcon size={18} />}
        title="Chat"
        text="Ask and explore, inside one project's code or across the board."
      />
      <Tile
        selected={blueprint}
        onSelect={() => onChange(true)}
        icon={<BlueprintIcon size={18} />}
        title="Blueprint"
        text="Turn a need into cards, stage by stage, with the architect."
      />
    </div>
  );
}

interface TileProps {
  selected: boolean;
  onSelect: () => void;
  icon: ReactNode;
  title: string;
  text: string;
}

function Tile({ selected, onSelect, icon, title, text }: TileProps) {
  const textId = useId();
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      aria-label={title}
      aria-describedby={textId}
      tabIndex={selected ? 0 : -1}
      className={`nd-kind${selected ? ' nd-kind--on' : ''}`}
      onClick={onSelect}
    >
      <span className="nd-kind-icon">{icon}</span>
      <span className="nd-kind-copy">
        <b>{title}</b>
        <span id={textId}>{text}</span>
      </span>
    </button>
  );
}
