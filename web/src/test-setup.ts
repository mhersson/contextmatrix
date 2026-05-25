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

// In-memory localStorage polyfill. Required on Node 25+ where the runtime no
// longer exposes a native localStorage by default (only when started with
// --localstorage-file), and jsdom 29 does not install one onto the global
// JSDOM window in that environment. Without this, any test that touches
// localStorage (directly or via a hook like useRailSync, useChatFilterPrefs)
// throws ReferenceError before the test body runs.
if (typeof globalThis.localStorage === 'undefined') {
  const store = new Map<string, string>();
  const polyfill: Storage = {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, String(value));
    },
  };
  Object.defineProperty(globalThis, 'localStorage', {
    value: polyfill,
    configurable: true,
    writable: false,
  });
  if (typeof window !== 'undefined' && !window.localStorage) {
    Object.defineProperty(window, 'localStorage', {
      value: polyfill,
      configurable: true,
      writable: false,
    });
  }
}

// Reset localStorage between tests so persisted-pref hooks (useChatFilterPrefs,
// useChatLayout, useTheme, etc.) start each test from defaults. Without this,
// writes from one `it()` block leak into the next via the shared storage.
afterEach(() => {
  if (typeof localStorage !== 'undefined' && typeof localStorage.clear === 'function') {
    localStorage.clear();
  }
});
