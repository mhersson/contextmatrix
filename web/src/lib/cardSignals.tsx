import type { ReactNode } from 'react';
import type { Card, WorkerStatus } from '../types';

/**
 * Signal metadata for the board card's header icon cluster. Lives outside the
 * component file so CardItem can share the signal count (for its crowded-header
 * type-pill treatment) without the component file exporting non-components.
 */
export interface CardSignal {
  key: string;
  label: string;
  color: string;
  icon: ReactNode;
  pulse?: boolean;
  /** Lower survives the header cap first. */
  importance: number;
}

/** Signals at or above this count switch the header into crowded mode. */
export const HEADER_SIGNAL_CAP = 4;

/** Shared stroke-icon attributes so the hovercard's rows match the header cluster. */
export const signalSvgProps = {
  width: 14,
  height: 14,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.9,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
} as const;

const botIcon = (
  <svg {...signalSvgProps}>
    <path d="M12 8V4H8" />
    <rect width="16" height="12" x="4" y="8" rx="2" />
    <path d="M2 14h2" />
    <path d="M20 14h2" />
    <path d="M15 13v2" />
    <path d="M9 13v2" />
  </svg>
);

const usersIcon = (
  <svg {...signalSvgProps}>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
    <path d="M16 3.13a4 4 0 0 1 0 7.75" />
  </svg>
);

const activityIcon = (
  <svg {...signalSvgProps}>
    <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
  </svg>
);

const alertTriangleIcon = (
  <svg {...signalSvgProps}>
    <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 20h16a2 2 0 0 0 1.73-2Z" />
    <path d="M12 9v4" />
    <path d="M12 17h.01" />
  </svg>
);

const clockIcon = (
  <svg {...signalSvgProps}>
    <circle cx="12" cy="12" r="10" />
    <path d="M12 6v6l4 2" />
  </svg>
);

const circleSlashIcon = (
  <svg {...signalSvgProps}>
    <circle cx="12" cy="12" r="10" />
    <path d="m4.9 4.9 14.2 14.2" />
  </svg>
);

const circleParkingIcon = (
  <svg {...signalSvgProps}>
    <circle cx="12" cy="12" r="10" />
    <path d="M9 17V7h4a3 3 0 0 1 0 6H9" />
  </svg>
);

const bookOpenIcon = (
  <svg {...signalSvgProps}>
    <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
    <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
  </svg>
);

const linkIcon = (
  <svg {...signalSvgProps}>
    <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
    <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
  </svg>
);

const trophyIcon = (
  <svg {...signalSvgProps}>
    <path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6" />
    <path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18" />
    <path d="M4 22h16" />
    <path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22" />
    <path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22" />
    <path d="M18 2H6v7a6 6 0 0 0 12 0V2Z" />
  </svg>
);

const featherIcon = (
  <svg {...signalSvgProps}>
    <path d="M12.67 19a2 2 0 0 0 1.42-.59l6.15-6.17a6 6 0 0 0-8.49-8.49L5.59 9.91A2 2 0 0 0 5 11.33V18a1 1 0 0 0 1 1z" />
    <path d="M16 8 2 22" />
    <path d="M17.5 15H9" />
  </svg>
);

const workerSignals: Record<WorkerStatus, Omit<CardSignal, 'key' | 'importance'>> = {
  queued: { label: 'Worker queued', color: 'var(--yellow)', icon: clockIcon },
  running: { label: 'Worker running', color: 'var(--aqua)', icon: activityIcon, pulse: true },
  failed: { label: 'Worker failed - open card for the log', color: 'var(--red)', icon: alertTriangleIcon },
  killed: { label: 'Worker killed', color: 'var(--grey1)', icon: circleSlashIcon },
  parked: { label: 'Parked - left for a human, see the card log', color: 'var(--yellow)', icon: circleParkingIcon },
};

/** Builds the card's signals in display order. */
export function cardSignals(card: Card): CardSignal[] {
  const signals: CardSignal[] = [];

  if ((card.depends_on?.length ?? 0) > 0) {
    // blocked_by is server-computed; without it the generic wording is the
    // only claim that is certainly true.
    const blocked = card.blocked_by?.length
      ? `Blocked by ${card.blocked_by.join(', ')}`
      : 'Blocked by dependencies';
    signals.push(
      card.dependencies_met
        ? { key: 'deps', label: 'All dependencies met', color: 'var(--green)', icon: linkIcon, importance: 1 }
        : { key: 'deps', label: blocked, color: 'var(--red)', icon: linkIcon, importance: 1 },
    );
  }
  if (card.autonomous) {
    signals.push({ key: 'auto', label: 'Autonomous', color: 'var(--purple)', icon: botIcon, importance: 2 });
  }
  if ((card.mob_participants ?? 0) >= 2) {
    signals.push({
      key: 'mob',
      label: `Mob session - ${card.mob_participants} agents`,
      color: 'var(--purple)',
      icon: usersIcon,
      importance: 3,
    });
  }
  // Suppressed when the card's mob session covers the execute phase: mob
  // coding takes priority and the race will not run.
  const showBestOfN =
    card.best_of_n != null &&
    card.best_of_n >= 2 &&
    !((card.mob_participants ?? 0) >= 2 && (card.mob_phases ?? []).includes('execute'));
  if (showBestOfN) {
    signals.push({
      key: 'best-of-n',
      label: `Best of ${card.best_of_n} - candidates judged, best one adopted`,
      color: 'var(--purple)',
      icon: trophyIcon,
      importance: 4,
    });
  }
  if (card.worker_status && workerSignals[card.worker_status]) {
    signals.push({ key: 'worker', importance: 0, ...workerSignals[card.worker_status] });
  }
  if (card.in_playbooks && card.in_playbooks.length > 0) {
    const label =
      card.in_playbooks.length === 1
        ? `In playbook: ${card.in_playbooks[0]}`
        : `In playbooks: ${card.in_playbooks.join(', ')}`;
    signals.push({ key: 'playbook', label, color: 'var(--blue)', icon: bookOpenIcon, importance: 5 });
  }
  if (card.labels?.includes('simple')) {
    signals.push({
      key: 'simple',
      label: 'Simple - no decomposition',
      color: 'var(--green)',
      icon: featherIcon,
      importance: 6,
    });
  }

  return signals;
}

/**
 * Caps the header cluster at HEADER_SIGNAL_CAP icons. The most important
 * signals survive (worker status and deps never hide); the rest collapse
 * behind a "+N" chip. Both halves keep display order.
 */
export function splitCardSignals(signals: CardSignal[]): { visible: CardSignal[]; hidden: CardSignal[] } {
  if (signals.length <= HEADER_SIGNAL_CAP) return { visible: signals, hidden: [] };

  const ranked = signals
    .map((signal, index) => ({ signal, index }))
    .sort((a, b) => a.signal.importance - b.signal.importance);
  const keep = new Set(ranked.slice(0, HEADER_SIGNAL_CAP).map((r) => r.index));

  return {
    visible: signals.filter((_, i) => keep.has(i)),
    hidden: signals.filter((_, i) => !keep.has(i)),
  };
}
