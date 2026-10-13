import { describe, it, expect, vi, beforeAll } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CardPanelEditor } from './CardPanelEditor';
import {
  RAW_HTML_BODY,
  RICH_HTML_BODY,
  SAFE_MARKDOWN_BODY,
  expectMarkdownRendered,
  expectRawHtmlInert,
  expectRichHtmlRendered,
} from '../../test-utils/markdownSafety';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark' }),
}));

// Both surfaces render the real markdown library. Suspense fallbacks show the
// raw body as plain text, so each helper waits for the rendered markdown root.
// The cold chunk imports are paid up front so they cannot outlast a waitFor.
beforeAll(() => Promise.all([import('../Markdown/SafeMarkdownPreview'), import('../Markdown/SafeMDEditor')]));

async function renderReadOnly(body: string): Promise<HTMLElement> {
  const { container } = render(
    <CardPanelEditor body={body} project="test" onChange={() => {}} editable editing={false} />,
  );
  return waitFor(() => {
    const root = container.querySelector<HTMLElement>('.wmde-markdown');
    expect(root).not.toBeNull();
    return root!;
  });
}

async function renderEditorPreview(body: string): Promise<HTMLElement> {
  const { container } = render(
    <CardPanelEditor body={body} project="test" onChange={() => {}} editable editing />,
  );
  fireEvent.click(await screen.findByRole('button', { name: /^Preview code/ }));
  return waitFor(() => {
    const root = container.querySelector<HTMLElement>('.w-md-editor-preview .wmde-markdown');
    expect(root).not.toBeNull();
    return root!;
  });
}

describe('CardPanelEditor read-only view', () => {
  it('renders raw HTML as inert text', async () => {
    expectRawHtmlInert(await renderReadOnly(RAW_HTML_BODY));
  });

  it('renders headings, links and code', async () => {
    expectMarkdownRendered(await renderReadOnly(SAFE_MARKDOWN_BODY));
  });

  it('renders allowlisted HTML', async () => {
    expectRichHtmlRendered(await renderReadOnly(RICH_HTML_BODY));
  });
});

describe('CardPanelEditor editor preview', () => {
  it('renders raw HTML as inert text', async () => {
    expectRawHtmlInert(await renderEditorPreview(RAW_HTML_BODY));
  });

  it('renders headings, links and code', async () => {
    expectMarkdownRendered(await renderEditorPreview(SAFE_MARKDOWN_BODY));
  });

  it('renders allowlisted HTML', async () => {
    expectRichHtmlRendered(await renderEditorPreview(RICH_HTML_BODY));
  });
});
