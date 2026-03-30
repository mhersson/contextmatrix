import { useState, useCallback } from 'react';
import type { Card } from '../../types';

interface ParentSearchProps {
  parent: string;
  setParent: (v: string) => void;
  cards: Card[];
}

export function ParentSearch({ parent, setParent, cards }: ParentSearchProps) {
  const [parentSearch, setParentSearch] = useState('');
  const [showDropdown, setShowDropdown] = useState(false);

  const filteredCards = parentSearch
    ? cards.filter((c) => {
        const q = parentSearch.toLowerCase();
        return c.id.toLowerCase().includes(q) || c.title.toLowerCase().includes(q);
      }).slice(0, 8)
    : [];

  const handleClear = useCallback(() => {
    setParent('');
    setParentSearch('');
  }, [setParent]);

  const handleSelect = useCallback((id: string) => {
    setParent(id);
    setParentSearch('');
    setShowDropdown(false);
  }, [setParent]);

  return (
    <div className="relative">
      <label className="block text-xs text-[var(--grey1)] mb-1">Parent Card</label>
      {parent ? (
        <div className="flex items-center gap-2 px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)]">
          <span className="font-mono text-sm text-[var(--aqua)]">{parent}</span>
          <button
            onClick={handleClear}
            className="text-[var(--grey1)] hover:text-[var(--red)] transition-colors text-xs"
          >
            x
          </button>
        </div>
      ) : (
        <input
          type="text"
          value={parentSearch}
          onChange={(e) => { setParentSearch(e.target.value); setShowDropdown(true); }}
          onFocus={() => setShowDropdown(true)}
          onBlur={() => setTimeout(() => setShowDropdown(false), 150)}
          placeholder="Search by ID or title..."
          className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
        />
      )}
      {showDropdown && filteredCards.length > 0 && !parent && (
        <div className="absolute z-10 w-full mt-1 rounded bg-[var(--bg2)] border border-[var(--bg3)] shadow-lg max-h-[200px] overflow-y-auto">
          {filteredCards.map((c) => (
            <button
              key={c.id}
              onMouseDown={() => handleSelect(c.id)}
              className="w-full text-left px-3 py-2 hover:bg-[var(--bg3)] transition-colors flex items-center gap-2"
            >
              <span className="font-mono text-xs text-[var(--grey1)]">{c.id}</span>
              <span className="text-sm text-[var(--fg)] truncate">{c.title}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
