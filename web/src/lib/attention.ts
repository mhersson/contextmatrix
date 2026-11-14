import type { AttentionItem } from '../types';

/** Where an attention item opens. */
export function attentionHref(item: AttentionItem): string {
  const id = encodeURIComponent(item.id);
  const project = encodeURIComponent(item.project ?? '');
  switch (item.kind) {
    case 'blueprint':
      return `/chat/${id}`;
    case 'playbook':
      return `/playbooks/${id}`;
    case 'card':
      return `/projects/${project}?card=${id}`;
    case 'sentinel':
      return `/projects/${project}?card=${id}&tab=sentinel`;
  }
}

/** Browser tab title: the waiting count leads while anything waits. */
export function tabTitle(n: number): string {
  return n > 0 ? `(${n}) ContextMatrix` : 'ContextMatrix';
}

/** Waiting card and Sentinel items per project. */
export function projectCounts(items: readonly AttentionItem[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const it of items) {
    if ((it.kind === 'card' || it.kind === 'sentinel') && it.project) {
      counts.set(it.project, (counts.get(it.project) ?? 0) + 1);
    }
  }
  return counts;
}
