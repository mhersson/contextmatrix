import type { CardCost } from '../types';

/** Filter cards by card ID (case-insensitive substring match). */
export function filterCardCosts(cards: CardCost[], search: string): CardCost[] {
  const q = search.trim().toLowerCase();
  if (!q) return cards;
  return cards.filter((c) => c.card_id.toLowerCase().includes(q));
}
