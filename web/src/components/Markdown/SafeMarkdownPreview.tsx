import MarkdownPreview from '@uiw/react-markdown-preview';
import { safeMarkdownOptions } from './safeMarkdownOptions';

interface SafeMarkdownPreviewProps {
  source: string;
  className?: string;
}

/** The only sanctioned MarkdownPreview: card bodies and chat come from agents
 *  and external imports, so raw HTML must never render as live elements. */
export default function SafeMarkdownPreview({ source, className }: SafeMarkdownPreviewProps) {
  return <MarkdownPreview source={source} className={className} {...safeMarkdownOptions} />;
}
