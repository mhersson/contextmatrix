import { Suspense, lazy, memo } from 'react';
import { safeUrlTransform } from '../../utils/safeUrlTransform';
import { ErrorBoundary } from '../ErrorBoundary';

// Lazy-load the markdown previewer so the chat panel doesn't pay the
// bundle cost until first use. The chat markdown styling is fully driven by
// CSS custom properties, so dark/light switches automatically without
// data-color-mode. The per-message Suspense fallback shows the raw source,
// so the transcript reads as plain text during the (preloaded, one-off)
// chunk fetch instead of blanking behind a single transcript-level boundary.
const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

function ChatMarkdownImpl({ source }: { source: string }) {
  const plainText = <div className="whitespace-pre-wrap break-words text-sm">{source}</div>;
  // The ErrorBoundary keeps a rejected markdown-chunk import (or a
  // pathological message) contained to this one bubble as plain text instead
  // of throwing to the panel-wide boundary and replacing the whole drawer.
  // No retry control: React.lazy caches a rejected import, so a retry could
  // only re-throw - a page reload recovers.
  return (
    <div className="bf-chat-markdown">
      <ErrorBoundary fallback={plainText}>
        <Suspense fallback={plainText}>
          <MarkdownPreview source={source} skipHtml urlTransform={safeUrlTransform} />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

/** Memoized so appends elsewhere in the transcript never re-run the unified
 *  markdown pipeline for already-rendered messages - react-markdown rebuilds
 *  its whole processor on every render, which is the dominant per-row cost. */
export const ChatMarkdown = memo(ChatMarkdownImpl);
