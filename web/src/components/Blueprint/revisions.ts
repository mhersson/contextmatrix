import { diffArrays, diffWords } from 'diff';

/** Markdown blocks: split at blank lines outside fenced code. */
export function splitBlocks(md: string): string[] {
  const blocks: string[] = [];
  let current: string[] = [];
  let fence: string | null = null;
  for (const line of md.replace(/\r\n/g, '\n').split('\n')) {
    if (fence === null && line.trim() === '') {
      if (current.length > 0) blocks.push(current.join('\n'));
      current = [];
      continue;
    }
    current.push(line);
    const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1];
    if (!marker) continue;
    if (fence === null) fence = marker;
    else if (marker[0] === fence[0] && marker.length >= fence.length) fence = null;
  }
  if (current.length > 0) blocks.push(current.join('\n'));
  return blocks;
}

const squash = (s: string) => s.replace(/\s+/g, ' ').trim();
const sameBlock = (a: string, b: string) => squash(a) === squash(b);

/** Indexes of `next` blocks that are new or edited against `prev`. */
export function changedBlocks(prev: readonly string[], next: readonly string[]): Set<number> {
  const changed = new Set<number>();
  let index = 0;
  for (const part of diffArrays([...prev], [...next], { comparator: sameBlock })) {
    if (part.removed) continue;
    if (part.added) part.value.forEach((_, k) => changed.add(index + k));
    index += part.value.length;
  }
  return changed;
}

export type BlockPart =
  | { kind: 'same' | 'added' | 'removed'; text: string }
  | { kind: 'changed'; before: string; after: string };

/** Block comparison; a removed run followed by an added run pairs up as changed blocks. */
export function compareBlocks(prev: readonly string[], next: readonly string[]): BlockPart[] {
  const parts = diffArrays([...prev], [...next], { comparator: sameBlock });
  const out: BlockPart[] = [];
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i];
    const following = parts[i + 1];
    if (part.removed && following?.added) {
      const paired = Math.min(part.value.length, following.value.length);
      for (let k = 0; k < paired; k++) out.push({ kind: 'changed', before: part.value[k], after: following.value[k] });
      for (const text of part.value.slice(paired)) out.push({ kind: 'removed', text });
      for (const text of following.value.slice(paired)) out.push({ kind: 'added', text });
      i++;
      continue;
    }
    const kind = part.added ? 'added' : part.removed ? 'removed' : 'same';
    for (const text of part.value) out.push({ kind, text });
  }
  return out;
}

export interface WordPart {
  kind: 'same' | 'added' | 'removed';
  text: string;
}

export function wordDiff(before: string, after: string): WordPart[] {
  return diffWords(before, after).map((c) => ({
    kind: c.added ? 'added' : c.removed ? 'removed' : 'same',
    text: c.value,
  }));
}

/** Ids of items that are new or differ from the item with the same id before. */
export function changedById<T extends { id: string }>(prev: readonly T[], next: readonly T[]): Set<string> {
  const before = new Map(prev.map((item) => [item.id, JSON.stringify(item)]));
  return new Set(next.filter((item) => before.get(item.id) !== JSON.stringify(item)).map((item) => item.id));
}
