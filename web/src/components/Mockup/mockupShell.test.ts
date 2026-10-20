/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, it, expect } from 'vitest';
import { RENDER_MESSAGE, SETTINGS_EVENT, SETTINGS_MESSAGE } from './frameProtocol';

const html = readFileSync(resolve(process.cwd(), 'public/mockup-frame.html'), 'utf8');
const script = /<script>([\s\S]*)<\/script>/.exec(html)![1];

type Listener = (e: { data: unknown; source: unknown }) => void;

function loadShell() {
  const parent = {};
  let listeners: Listener[] = [];
  const written: { html: string; settings: unknown }[] = [];
  const events: { type: string; detail: unknown }[] = [];
  const win: Record<string, unknown> = {
    parent,
    addEventListener: (type: string, fn: Listener) => type === 'message' && listeners.push(fn),
    removeEventListener: (_type: string, fn: Listener) => (listeners = listeners.filter((l) => l !== fn)),
    dispatchEvent: (e: { type: string; detail: unknown }) => events.push(e),
  };
  const doc = {
    open: () => (listeners = []),
    write: (h: string) => written.push({ html: h, settings: win.mockupSettings }),
    close: () => {},
  };
  class FakeCustomEvent {
    type: string;
    detail: unknown;
    constructor(type: string, init: { detail: unknown }) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  new Function('window', 'document', 'CustomEvent', script)(win, doc, FakeCustomEvent);
  const post = (data: unknown, source: unknown = parent) => [...listeners].forEach((l) => l({ data, source }));
  return { win, written, events, post };
}

describe('mockup frame shell', () => {
  it('writes the source its parent posts, with the settings in place before it runs', () => {
    const { written, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: '<p>shop</p>', settings: { layout: 'cards' } });
    expect(written).toEqual([{ html: '<p>shop</p>', settings: { layout: 'cards' } }]);
  });

  it('ignores a render from anything but its parent, and a malformed one', () => {
    const { written, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: '<p>x</p>' }, {});
    post({ type: RENDER_MESSAGE, source: 42 });
    post({ type: 'other', source: '<p>x</p>' });
    expect(written).toEqual([]);
  });

  it('renders only the first source it is given', () => {
    const { written, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: 'first' });
    post({ type: RENDER_MESSAGE, source: 'second' });
    expect(written.map((w) => w.html)).toEqual(['first']);
  });

  it('hands every later settings change to the mockup as the mockup:settings event', () => {
    const { win, events, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: 'x' });
    post({ type: SETTINGS_MESSAGE, settings: { layout: 'rows' } });
    expect(win.mockupSettings).toEqual({ layout: 'rows' });
    expect(events).toHaveLength(1);
    expect(events[0].type).toBe(SETTINGS_EVENT);
    expect(events[0].detail).toEqual({ layout: 'rows' });
  });

  it('ignores settings from anything but its parent', () => {
    const { win, events, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: 'x', settings: { layout: 'cards' } });
    post({ type: SETTINGS_MESSAGE, settings: { layout: 'rows' } }, {});
    expect(win.mockupSettings).toEqual({ layout: 'cards' });
    expect(events).toEqual([]);
  });

  it('leaves the settings to a card source that fixes its own', () => {
    const { written, post } = loadShell();
    post({ type: RENDER_MESSAGE, source: 'x' });
    expect(written[0].settings).toBeUndefined();
  });

  it('ignores an empty post from its parent', () => {
    const { written, events, post } = loadShell();
    expect(() => {
      post(null);
      post(undefined);
    }).not.toThrow();
    expect(written).toEqual([]);
    expect(events).toEqual([]);
  });

  it('ignores settings sent before any render', () => {
    const { win, events, post } = loadShell();
    post({ type: SETTINGS_MESSAGE, settings: { layout: 'rows' } });
    expect(win.mockupSettings).toBeUndefined();
    expect(events).toEqual([]);
  });
});
