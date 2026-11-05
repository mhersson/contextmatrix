import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, act } from '@testing-library/react';
import { MockupFrame } from './MockupFrame';
import { MOCKUP_FRAME_PATH, RENDER_MESSAGE, SETTINGS_MESSAGE } from './frameProtocol';

const SRC = '<!doctype html><p>hello</p>';
const declare = (width: string) => `<!doctype html><meta name="viewport" content="width=${width}"><p>hello</p>`;

/** Records each observer so a test can report a pane size the way a browser would. */
class StubResizeObserver {
  static live = new Set<StubResizeObserver>();
  targets: Element[] = [];
  private callback: ResizeObserverCallback;
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
    StubResizeObserver.live.add(this);
  }
  observe(target: Element) {
    this.targets.push(target);
  }
  unobserve() {}
  disconnect() {
    StubResizeObserver.live.delete(this);
  }
  report(width: number, height: number) {
    const entries = this.targets.map((target) => ({ target, contentRect: { width, height } }));
    this.callback(entries as unknown as ResizeObserverEntry[], this as unknown as ResizeObserver);
  }
}

function resizePane(width: number, height: number) {
  act(() => StubResizeObserver.live.forEach((observer) => observer.report(width, height)));
}

function mount(props: { source?: string; settings?: Record<string, string> } = {}) {
  const source = props.source ?? SRC;
  const view = render(<MockupFrame source={source} settings={props.settings} title="t" />);
  const frame = () => view.container.querySelector('iframe')!;
  const spyOn = (el: HTMLIFrameElement) => vi.spyOn(el.contentWindow!, 'postMessage');
  return { ...view, frame, spyOn };
}

beforeEach(() => {
  StubResizeObserver.live.clear();
  vi.stubGlobal('ResizeObserver', StubResizeObserver);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

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

describe('MockupFrame scaling', () => {
  const unscaled = (frame: HTMLIFrameElement) => {
    expect(frame.style.width).toBe('');
    expect(frame.style.height).toBe('');
    expect(frame.style.transform).toBe('');
    expect(frame.style.transformOrigin).toBe('');
  };

  it('puts the frame in a pane that carries the look and the size', () => {
    const { frame } = mount();
    expect(frame().parentElement).toHaveClass('mk-frame');
    expect(frame()).toHaveClass('mk-frame-doc');
  });

  it('leaves a source without a declaration at the pane size', () => {
    const { frame } = mount();
    resizePane(640, 560);
    unscaled(frame());
  });

  it('draws a declared width wider than the pane at that width and scales it down', () => {
    const { frame } = mount({ source: declare('1280') });
    resizePane(640, 560);
    expect(frame().style.width).toBe('1280px');
    expect(frame().style.height).toBe('1120px');
    expect(frame().style.transform).toBe('scale(0.5)');
    expect(frame().style.transformOrigin).toMatch(/^0(px)? 0(px)?$/);
  });

  it('fills the pane exactly at a scale that is not a round number', () => {
    const { frame } = mount({ source: declare('1280') });
    resizePane(740, 558);
    const scale = 740 / 1280;
    expect(frame().style.transform).toBe(`scale(${scale})`);
    expect(parseFloat(frame().style.width) * scale).toBeCloseTo(740, 6);
    expect(parseFloat(frame().style.height) * scale).toBeCloseTo(558, 6);
  });

  it('never scales up: a declared width the pane already holds is left alone', () => {
    const narrow = mount({ source: declare('390') });
    resizePane(640, 560);
    unscaled(narrow.frame());
    narrow.unmount();

    const exact = mount({ source: declare('640') });
    resizePane(640, 560);
    unscaled(exact.frame());
  });

  it('clamps a declared width before comparing it to the pane', () => {
    const { frame } = mount({ source: declare('99999') });
    resizePane(1280, 560);
    expect(frame().style.width).toBe('2560px');
    expect(frame().style.transform).toBe('scale(0.5)');
  });

  it('stays unscaled until the pane has reported a size', () => {
    const { frame } = mount({ source: declare('1280') });
    unscaled(frame());
    resizePane(0, 0);
    unscaled(frame());
  });

  it('rescales when the pane resizes, and drops the scale once the pane is wide enough', () => {
    const { frame } = mount({ source: declare('1280') });
    resizePane(640, 560);
    expect(frame().style.transform).toBe('scale(0.5)');
    resizePane(320, 400);
    expect(frame().style.transform).toBe('scale(0.25)');
    expect(frame().style.height).toBe('1600px');
    resizePane(1280, 560);
    unscaled(frame());
  });

  it('follows the source: a new declaration takes the new pane, a missing one unscales it', () => {
    const { frame, rerender } = mount({ source: declare('1280') });
    resizePane(640, 560);
    expect(frame().style.width).toBe('1280px');
    rerender(<MockupFrame source={declare('1600')} title="t" />);
    resizePane(640, 560);
    expect(frame().style.width).toBe('1600px');
    expect(frame().style.transform).toBe('scale(0.4)');
    rerender(<MockupFrame source={SRC} title="t" />);
    resizePane(640, 560);
    unscaled(frame());
  });

  it('observes the pane, not the frame, and stops when it unmounts', () => {
    const { frame, unmount } = mount({ source: declare('1280') });
    const [observer] = [...StubResizeObserver.live];
    expect(observer.targets).toEqual([frame().parentElement]);
    unmount();
    expect(StubResizeObserver.live.size).toBe(0);
  });

  it('still hands the source over to a scaled frame', () => {
    const source = declare('1280');
    const { frame, spyOn } = mount({ source });
    resizePane(640, 560);
    const post = spyOn(frame());
    fireEvent.load(frame());
    expect(post.mock.calls).toEqual([[{ type: RENDER_MESSAGE, source }, '*']]);
  });
});
