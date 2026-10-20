import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import SafeMarkdownPreview from '../Markdown/SafeMarkdownPreview';
import { MOCKUP_LABELS } from './labels';
import { RENDER_MESSAGE } from './frameProtocol';
import { RAW_HTML_BODY } from '../../test-utils/markdownSafety';

beforeAll(() => import('../Markdown/SafeMarkdownPreview'));
afterEach(() => vi.restoreAllMocks());

const SRC = '<!doctype html>\n<html><body><h1>Sign in</h1></body></html>';
const fence = (src: string, ticks = '```') => `${ticks}mockup\n${src}\n${ticks}\n`;

async function show(body: string, mockups = true) {
  const view = render(<SafeMarkdownPreview source={body} mockups={mockups} />);
  await waitFor(() => expect(view.container.querySelector('.wmde-markdown')).not.toBeNull());
  return view;
}

function postedSource(frame: HTMLIFrameElement): string {
  const post = vi.spyOn(frame.contentWindow!, 'postMessage');
  fireEvent.load(frame);
  const msg = post.mock.calls[0][0] as { type: string; source: string };
  expect(msg.type).toBe(RENDER_MESSAGE);
  return msg.source;
}

describe('MockupBlock', () => {
  it('shows a mockup block behind Show mockup and runs it sealed on click', async () => {
    const { container } = await show('Intro\n\n' + fence(SRC));
    expect(container.querySelector('iframe')).toBeNull();
    const button = screen.getByRole('button', { name: MOCKUP_LABELS.show });
    expect(button).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(button);
    const frames = container.querySelectorAll('iframe');
    expect(frames).toHaveLength(1);
    expect(frames[0].getAttribute('sandbox')).toBe('allow-scripts');
    expect(postedSource(frames[0]).trimEnd()).toBe(SRC);
    const hide = screen.getByRole('button', { name: MOCKUP_LABELS.hide });
    expect(hide).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(hide);
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('keeps a hostile block inert outside the frame and hands it over verbatim', async () => {
    const SRC_HOSTILE =
      '<!doctype html><p>a</p></code></pre><img src=x onerror=alert(1)><script>parent.alert(1)</script>';
    const { container } = await show(fence(SRC_HOSTILE) + '\n' + RAW_HTML_BODY);
    const inert = () => {
      expect(container.querySelectorAll('script, object, embed, form, img[onerror]')).toHaveLength(0);
      for (const el of container.querySelectorAll('*')) {
        expect([...el.attributes].filter((a) => a.name.startsWith('on'))).toEqual([]);
      }
      expect(container.querySelectorAll('.mk-block img')).toHaveLength(0);
      expect((window as { markdownXss?: unknown }).markdownXss).toBeUndefined();
    };
    inert();
    expect(container.querySelector('iframe')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: MOCKUP_LABELS.show }));
    inert();
    const frames = container.querySelectorAll('iframe');
    expect(frames).toHaveLength(1);
    expect(frames[0].getAttribute('sandbox')).toBe('allow-scripts');
    expect(postedSource(frames[0]).trimEnd()).toBe(SRC_HOSTILE);
  });

  it('takes a source holding a backtick run inside a longer fence', async () => {
    const src = '<p>one</p>\n```\n<p>two</p>';
    const { container } = await show(fence(src, '````'));
    fireEvent.click(screen.getByRole('button', { name: MOCKUP_LABELS.show }));
    expect(postedSource(container.querySelector('iframe')!).trimEnd()).toBe(src);
  });

  it('renders several blocks, each behind its own button', async () => {
    const { container } = await show(fence('<p>a</p>') + '\ntext\n\n' + fence('<p>b</p>'));
    const buttons = screen.getAllByRole('button', { name: MOCKUP_LABELS.show });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[1]);
    expect(container.querySelectorAll('iframe')).toHaveLength(1);
    expect(postedSource(container.querySelector('iframe')!).trimEnd()).toBe('<p>b</p>');
  });

  it('shows the block as code without the mockups prop', async () => {
    const { container } = await show(fence(SRC), false);
    expect(screen.queryByRole('button', { name: MOCKUP_LABELS.show })).toBeNull();
    expect(container.querySelector('code.language-mockup')?.textContent).toContain(SRC.slice(0, 15));
  });
});
