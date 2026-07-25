import { Suspense, lazy, memo } from 'react';
import { safeUrlTransform } from '../../utils/safeUrlTransform';

// Lazy-load the markdown previewer so the chat panel doesn't pay the
// bundle cost until first use. The chat markdown styling is fully driven by
// CSS custom properties, so dark/light switches automatically without
// data-color-mode. The per-message Suspense fallback shows the raw source,
// so the transcript reads as plain text during the (preloaded, one-off)
// chunk fetch instead of blanking behind a single transcript-level boundary.
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

function ChatMarkdownImpl({ source }: { source: string }) {
  return (
    <div className="bf-chat-markdown">
      <Suspense fallback={<div className="whitespace-pre-wrap break-words text-sm">{source}</div>}>
        <MarkdownPreview source={source} skipHtml urlTransform={safeUrlTransform} />
      </Suspense>
    </div>
  );
}

/** Memoized so appends elsewhere in the transcript never re-run the unified
 *  markdown pipeline for already-rendered messages - react-markdown rebuilds
 *  its whole processor on every render, which is the dominant per-row cost. */
export const ChatMarkdown = memo(ChatMarkdownImpl);
