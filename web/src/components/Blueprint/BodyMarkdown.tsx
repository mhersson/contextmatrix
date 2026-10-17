import { memo, useMemo } from 'react';
import { ChatMarkdown } from '../ChatPanel/ChatMarkdown';
import { DiffText } from './DiffText';
import { RevisionCloud } from './RevisionCloud';
import { changedBlocks, compareBlocks, splitBlocks, type BlockPart } from './revisions';

export type RevisionMode = 'plain' | 'clouds' | 'compare';

interface BodyMarkdownProps {
  body: string;
  /** The previous version's body; clouds and compare need it. */
  previous?: string;
  mode: RevisionMode;
  /** Numbers each cloud's triangle. */
  version: number;
}

/** Artifact markdown rendered block by block, so changed paragraphs can be clouded or diffed. */
export function BodyMarkdown({ body, previous, mode, version }: BodyMarkdownProps) {
  const blocks = useMemo(() => splitBlocks(body), [body]);
  const before = useMemo(() => (previous === undefined ? null : splitBlocks(previous)), [previous]);
  if (mode === 'compare' && before) {
    return (
      <div className="bp-body">
        {compareBlocks(before, blocks).map((part, i) => (
          <ComparePart key={i} part={part} />
        ))}
      </div>
    );
  }
  if (blocks.length === 0) return null;
  const changed = mode === 'clouds' && before ? changedBlocks(before, blocks) : null;
  return (
    <div className="bp-body">
      {blocks.map((text, i) =>
        changed?.has(i) ? (
          <RevisionCloud key={i} version={version}>
            <Block text={text} />
          </RevisionCloud>
        ) : (
          <Block key={i} text={text} />
        ),
      )}
    </div>
  );
}

export const Block = memo(function Block({ text }: { text: string }) {
  return <ChatMarkdown source={text} className="bp-markdown" />;
});

function ComparePart({ part }: { part: BlockPart }) {
  switch (part.kind) {
    case 'same':
      return <Block text={part.text} />;
    case 'added':
      return (
        <p className="bp-diff">
          <ins>{part.text}</ins>
        </p>
      );
    case 'removed':
      return (
        <p className="bp-diff">
          <del>{part.text}</del>
        </p>
      );
    case 'changed':
      return (
        <p className="bp-diff">
          <DiffText before={part.before} after={part.after} />
        </p>
      );
  }
}
