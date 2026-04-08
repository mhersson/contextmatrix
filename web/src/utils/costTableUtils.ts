import type { CardCost } from '../types';

/** Filter cards by card ID (case-insensitive substring match). */
export function filterCardCosts(cards: CardCost[], search: string): CardCost[] {
  const q = search.trim().toLowerCase();
  if (!q) return cards;
  return cards.filter((c) => c.card_id.toLowerCase().includes(q));
}

/** Slice an array for a given 1-based page and page size. */
export function paginateItems<T>(
  items: T[],
  page: number,
  pageSize: number,
): { items: T[]; totalPages: number } {
  if (items.length === 0) return { items: [], totalPages: 0 };
  const totalPages = Math.ceil(items.length / pageSize);
  const start = (page - 1) * pageSize;
  const end = start + pageSize;
  return { items: items.slice(start, end), totalPages };
}
