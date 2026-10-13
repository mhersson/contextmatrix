import { describe, it, expect, beforeAll } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { ChatMarkdown } from './ChatMarkdown';
import {
  RAW_HTML_BODY,
  RICH_HTML_BODY,
  SAFE_MARKDOWN_BODY,
  expectMarkdownRendered,
  expectRawHtmlInert,
  expectRichHtmlRendered,
} from '../../test-utils/markdownSafety';

// Renders the real markdown library: the Suspense fallback is plain text, so
// each test waits for the rendered markdown root before asserting. The cold
// chunk import is paid up front so it cannot outlast a waitFor under load.
beforeAll(() => import('../Markdown/SafeMarkdownPreview'));

async function renderMarkdown(source: string): Promise<HTMLElement> {
  const { container } = render(<ChatMarkdown source={source} />);
  return waitFor(() => {
    const root = container.querySelector<HTMLElement>('.wmde-markdown');
    expect(root).not.toBeNull();
    return root!;
  });
}

describe('ChatMarkdown', () => {
  it('renders raw HTML as inert text', async () => {
    expectRawHtmlInert(await renderMarkdown(RAW_HTML_BODY));
  });

  it('renders headings, links and code', async () => {
    expectMarkdownRendered(await renderMarkdown(SAFE_MARKDOWN_BODY));
  });

  it('renders allowlisted HTML', async () => {
    expectRichHtmlRendered(await renderMarkdown(RICH_HTML_BODY));
  });
});
