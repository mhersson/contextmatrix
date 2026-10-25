import MarkdownPreview from '@uiw/react-markdown-preview/common';
import { MockupAwarePre } from '../Mockup/MockupBlock';
import { safeMarkdownOptions } from './safeMarkdownOptions';

interface SafeMarkdownPreviewProps {
  source: string;
  className?: string;
  /** Cards only: a fenced mockup block becomes a button that opens it in the sealed frame. */
  mockups?: boolean;
}

const mockupComponents = { pre: MockupAwarePre };

/** The only sanctioned MarkdownPreview: card bodies and chat come from agents
 *  and external imports, so raw HTML passes an allowlist sanitizer and nothing
 *  outside it renders live. A fenced mockup block becomes CM's sealed frame,
 *  never live markup. */
export default function SafeMarkdownPreview({ source, className, mockups }: SafeMarkdownPreviewProps) {
  return (
    <MarkdownPreview
      source={source}
      className={className}
      components={mockups ? mockupComponents : undefined}
      {...safeMarkdownOptions}
    />
  );
}
