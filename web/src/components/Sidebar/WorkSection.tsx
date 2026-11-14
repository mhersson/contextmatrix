import { useId, useState, type ReactNode } from 'react';
import { Link } from 'react-router';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { safeGetString, safeSetString } from '../../utils/safeStorage';

interface WorkSectionProps {
  label: string;
  /** localStorage key remembering whether the section is collapsed. */
  storageKey: string;
  emptyText: string;
  allLabel: string;
  allHref: string;
  /** Rows the "All" link counts, shown or not. */
  total: number;
  /** Omitted: the section has no `+ new`. */
  onNew?: () => void;
  newLabel?: string;
  /** False until the list first loads, so an empty section makes no claim yet. */
  loaded?: boolean;
  /** The `<li>` rows; rendered when `total` > 0. */
  children: ReactNode;
}

export function WorkSection({
  label,
  storageKey,
  emptyText,
  allLabel,
  allHref,
  total,
  onNew,
  newLabel,
  loaded = true,
  children,
}: WorkSectionProps) {
  const mobileSidebar = useMobileSidebar();
  const headerId = useId();
  const listId = useId();
  const [collapsed, setCollapsed] = useState<boolean>(() => safeGetString(storageKey) === '1');

  const toggle = () => {
    setCollapsed((c) => {
      const next = !c;
      safeSetString(storageKey, next ? '1' : '0');
      return next;
    });
  };

  const handleNew = () => {
    onNew?.();
    mobileSidebar.close();
  };

  return (
    <section className="sb-chat-dock my-1.5 px-2 py-1" aria-labelledby={headerId}>
      <div className="flex items-center justify-between px-3 py-1">
        <button
          id={headerId}
          type="button"
          onClick={toggle}
          className="sb-eyebrow flex items-center gap-1.5"
          aria-expanded={!collapsed}
          aria-controls={listId}
        >
          <span aria-hidden="true" className="text-[8px]">{collapsed ? '▸' : '▼'}</span>
          {label}
        </button>
        {onNew && (
          <button type="button" onClick={handleNew} className="sb-eyebrow-btn" aria-label={newLabel} title={newLabel}>
            + new
          </button>
        )}
      </div>
      {!collapsed && (
        <div id={listId} className="mt-0.5 pb-0.5">
          {total === 0 ? (
            loaded && (
              <p className="px-3 py-2 text-xs italic" style={{ color: 'var(--grey1)' }}>
                {emptyText}
              </p>
            )
          ) : (
            <>
              <ul className="space-y-0.5">{children}</ul>
              <Link to={allHref} onClick={mobileSidebar.close} className="sb-all">
                <span>
                  {allLabel} <span aria-hidden="true">→</span>
                </span>
                <span className="sb-all__count">{total}</span>
              </Link>
            </>
          )}
        </div>
      )}
    </section>
  );
}
