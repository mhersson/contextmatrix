import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { MockupFrame } from './MockupFrame';
import { MOCKUP_FRAME_PATH, RENDER_MESSAGE, SETTINGS_MESSAGE } from './frameProtocol';

const SRC = '<!doctype html><p>hello</p>';

function mount(props: { source?: string; settings?: Record<string, string> } = {}) {
  const source = props.source ?? SRC;
  const view = render(<MockupFrame source={source} settings={props.settings} title="t" />);
  const frame = () => view.container.querySelector('iframe')!;
  const spyOn = (el: HTMLIFrameElement) => vi.spyOn(el.contentWindow!, 'postMessage');
  return { ...view, frame, spyOn };
}

afterEach(() => vi.restoreAllMocks());

describe('MockupFrame', () => {
  it('is sealed: scripts only, no same-origin, forms, popups or top navigation', () => {
    const { frame } = mount();
    expect(frame().getAttribute('sandbox')?.split(/\s+/)).toEqual(['allow-scripts']);
    expect(frame().getAttribute('src')).toBe(MOCKUP_FRAME_PATH);
    expect(frame().getAttribute('referrerpolicy')).toBe('no-referrer');
    expect(frame().hasAttribute('srcdoc')).toBe(false);
    expect(frame().hasAttribute('allow')).toBe(false);
  });

  it('hands the source and settings over once the shell has loaded', () => {
    const { frame, spyOn } = mount({ settings: { layout: 'rows' } });
    const post = spyOn(frame());
    expect(post).not.toHaveBeenCalled();
    fireEvent.load(frame());
    expect(post.mock.calls).toEqual([[{ type: RENDER_MESSAGE, source: SRC, settings: { layout: 'rows' } }, '*']]);
  });

  it('posts the source once even when the frame fires load again', () => {
    const { frame, spyOn } = mount();
    const post = spyOn(frame());
    fireEvent.load(frame());
    fireEvent.load(frame());
    expect(post).toHaveBeenCalledTimes(1);
  });

  it('sends each settings change without reloading the frame', () => {
    const { frame, spyOn, rerender } = mount({ settings: { layout: 'rows' } });
    const first = frame();
    const post = spyOn(first);
    rerender(<MockupFrame source={SRC} settings={{ layout: 'cards' }} title="t" />);
    expect(post).not.toHaveBeenCalled();
    fireEvent.load(first);
    expect(post.mock.calls).toEqual([[{ type: RENDER_MESSAGE, source: SRC, settings: { layout: 'cards' } }, '*']]);
    rerender(<MockupFrame source={SRC} settings={{ layout: 'rows' }} title="t" />);
    expect(frame()).toBe(first);
    expect(post.mock.calls.at(-1)).toEqual([{ type: SETTINGS_MESSAGE, settings: { layout: 'rows' } }, '*']);
  });

  it('loads a fresh frame for a new source', () => {
    const { frame, spyOn, rerender } = mount();
    const first = frame();
    rerender(<MockupFrame source="<p>other</p>" title="t" />);
    expect(frame()).not.toBe(first);
    const post = spyOn(frame());
    fireEvent.load(frame());
    expect(post.mock.calls).toEqual([[{ type: RENDER_MESSAGE, source: '<p>other</p>' }, '*']]);
  });

  it('listens to nothing the frame posts', () => {
    const add = vi.spyOn(window, 'addEventListener');
    const { frame } = mount({ settings: { a: 'b' } });
    fireEvent.load(frame());
    expect(add.mock.calls.some(([type]) => type === 'message')).toBe(false);
  });

  it("sends a card's source without settings", () => {
    const { frame, spyOn } = mount();
    const post = spyOn(frame());
    fireEvent.load(frame());
    expect('settings' in (post.mock.calls[0][0] as object)).toBe(false);
  });
});
