import { wordDiff } from './revisions';

export function DiffText({ before, after }: { before: string; after: string }) {
  return (
    <>
      {wordDiff(before, after).map((part, i) =>
        part.kind === 'added' ? (
          <ins key={i}>{part.text}</ins>
        ) : part.kind === 'removed' ? (
          <del key={i}>{part.text}</del>
        ) : (
          <span key={i}>{part.text}</span>
        ),
      )}
    </>
  );
}
