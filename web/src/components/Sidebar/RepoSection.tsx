import type { ReactNode } from 'react';
import type { SyncStatus } from '../../types';
import { SyncDot } from './SyncDot';

interface RepoSectionProps {
  name: string;
  shared: boolean;
  status: SyncStatus | null;
  collapsed: boolean;
  onToggle: () => void;
  children: ReactNode;
}

export function RepoSection({ name, shared, status, collapsed, onToggle, children }: RepoSectionProps) {
  return (
    <section aria-label={`Boards repo ${name}`}>
      <button
        type="button"
        className="sb-eyebrow sb-repo-header flex w-full items-center gap-2 px-3 pt-2 pb-1"
        onClick={onToggle}
        aria-expanded={!collapsed}
      >
        <span aria-hidden="true">{collapsed ? '▸' : '▾'}</span>
        <span className="truncate">{name}</span>
        {shared && <span className="sb-repo-tag">shared</span>}
        <span className="ml-auto flex"><SyncDot status={status} repo={name} /></span>
      </button>
      {!collapsed && children}
    </section>
  );
}
