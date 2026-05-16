import '@testing-library/jest-dom';
import { afterEach } from 'vitest';

// jsdom does not implement ResizeObserver; provide a no-op stub so components
// that use it (e.g. VirtualLogList) mount cleanly under tests that don't
// themselves override the global.
if (typeof globalThis.ResizeObserver === 'undefined') {
  class NoopResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  (globalThis as unknown as { ResizeObserver: typeof NoopResizeObserver }).ResizeObserver =
    NoopResizeObserver;
}

// Reset localStorage between tests so persisted-pref hooks (useChatFilterPrefs,
// useChatLayout, useTheme, etc.) start each test from defaults. Without this,
// writes from one `it()` block leak into the next via jsdom's shared storage —
// invisible on Node 22+ where localStorage is undefined, but breaks CI on
// Node 20 where jsdom 29 supplies a real implementation.
// On Node 25+ the runtime exposes a native localStorage (backed by
// --localstorage-file) that lacks .clear() unless a valid file path is given.
// Guard against that to avoid TypeError in afterEach when .clear is undefined.
afterEach(() => {
  if (typeof localStorage !== 'undefined' && typeof localStorage.clear === 'function') {
    localStorage.clear();
  }
});
