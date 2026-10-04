import { useCallback, useState } from 'react';
import type { SelectorCandidate } from '../../types';

export const PROVIDER_FILTER_KEY = 'contextmatrix-ladder-provider-filter';

export const OTHER_PROVIDER = 'other';

/** The provider grouping a pill belongs to: the creator, else the slug vendor prefix, else `other`. */
export function providerOf(c: Pick<SelectorCandidate, 'slug' | 'creator'>): string {
  const creator = c.creator.trim();
  if (creator) return creator;
  const slash = c.slug.indexOf('/');
  const prefix = slash > 0 ? c.slug.slice(0, slash) : '';
  return prefix || OTHER_PROVIDER;
}

export function providerCounts(candidates: ReadonlyArray<Pick<SelectorCandidate, 'slug' | 'creator'>>): Array<{ provider: string; count: number }> {
  const counts = new Map<string, number>();
  for (const c of candidates) {
    const provider = providerOf(c);
    counts.set(provider, (counts.get(provider) ?? 0) + 1);
  }
  return Array.from(counts, ([provider, count]) => ({ provider, count })).sort((a, b) =>
    a.provider.localeCompare(b.provider, undefined, { sensitivity: 'base' }),
  );
}

function loadHidden(): ReadonlySet<string> {
  try {
    const raw = localStorage.getItem(PROVIDER_FILTER_KEY);
    if (!raw) return new Set();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((entry): entry is string => typeof entry === 'string'));
  } catch {
    return new Set();
  }
}

function saveHidden(hidden: ReadonlySet<string>): void {
  try {
    localStorage.setItem(PROVIDER_FILTER_KEY, JSON.stringify(Array.from(hidden)));
  } catch {
    // Storage blocked (private mode, quota); the filter stays session-only.
  }
}

export interface ProviderFilterState {
  hidden: ReadonlySet<string>;
  toggle: (provider: string) => void;
  selectAll: () => void;
  selectNone: () => void;
  isActive: boolean;
  visible: (c: Pick<SelectorCandidate, 'slug' | 'creator'>) => boolean;
}

/**
 * The set of UNCHECKED providers, so a provider added to the catalog later
 * defaults to checked. Storage access is best-effort; an absent or throwing
 * localStorage leaves everything visible.
 */
export function useProviderFilter(candidates: ReadonlyArray<Pick<SelectorCandidate, 'slug' | 'creator'>>): ProviderFilterState {
  const [hidden, setHidden] = useState<ReadonlySet<string>>(loadHidden);

  const persist = useCallback((next: ReadonlySet<string>) => {
    setHidden(next);
    saveHidden(next);
  }, []);

  const toggle = useCallback(
    (provider: string) => {
      setHidden((prev) => {
        const next = new Set(prev);
        if (next.has(provider)) {
          next.delete(provider);
        } else {
          next.add(provider);
        }
        saveHidden(next);
        return next;
      });
    },
    [],
  );

  const selectAll = useCallback(() => persist(new Set()), [persist]);

  const selectNone = useCallback(() => {
    setHidden((prev) => {
      const next = new Set(prev);
      for (const c of candidates) next.add(providerOf(c));
      saveHidden(next);
      return next;
    });
  }, [candidates]);

  const providers = new Set(candidates.map(providerOf));
  const isActive = Array.from(providers).some((provider) => hidden.has(provider));
  const visible = useCallback((c: Pick<SelectorCandidate, 'slug' | 'creator'>) => !hidden.has(providerOf(c)), [hidden]);

  return { hidden, toggle, selectAll, selectNone, isActive, visible };
}
